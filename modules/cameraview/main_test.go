package main

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func viewerServer(t *testing.T, upstream string) *httptest.Server {
	t.Helper()
	oldCameras, oldManager := cameras, manager
	cameras = []Camera{{Name: "test camera", URL: upstream}}
	manager = &streamManager{streams: make(map[int]*cameraStream)}
	t.Cleanup(func() { cameras, manager = oldCameras, oldManager })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stream/{id}", streamHandler)
	server := httptest.NewServer(mux)
	server.Client().Timeout = 5 * time.Second
	t.Cleanup(server.Close)
	return server
}

func TestStreamPreservesCameraBoundaryAndCompleteBody(t *testing.T) {
	boundary := "camera.boundary-123"
	contentType := `multipart/x-mixed-replace; boundary="camera.boundary-123"`
	body := "--" + boundary + "\r\nContent-Type: image/jpeg\r\n\r\nfirst-image\r\n--" + boundary
	body += "\r\nContent-Type: image/jpeg\r\n\r\nsecond-image\r\n--" + boundary + "--\r\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		io.WriteString(w, body)
	}))
	defer upstream.Close()
	viewer := viewerServer(t, upstream.URL)
	response, err := viewer.Client().Get(viewer.URL + "/stream/0")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != contentType {
		t.Fatalf("received status %d and Content-Type %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	received, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(received) != body {
		t.Fatalf("received stream %q; expected %q", received, body)
	}
	reader := multipart.NewReader(bytes.NewReader(received), boundary)
	for _, expected := range []string{"first-image", "second-image"} {
		part, err := reader.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(part)
		if err != nil || string(data) != expected {
			t.Fatalf("received part %q, error %v; expected %q", data, err, expected)
		}
	}
	if _, err := reader.NextPart(); err != io.EOF {
		t.Fatalf("expected end of multipart stream, received %v", err)
	}
}

func TestCameraStartupFailureReturnsBadGateway(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      int
		contentType string
		expected    string
	}{
		{"upstream status", http.StatusServiceUnavailable, "text/plain", "HTTP 503"},
		{"wrong content type", http.StatusOK, "image/jpeg", "multipart/x-mixed-replace with a boundary"},
		{"missing boundary", http.StatusOK, "multipart/x-mixed-replace", "with a boundary"},
		{"malformed content type", http.StatusOK, `multipart/x-mixed-replace; boundary="`, "with a boundary"},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", test.contentType)
				w.WriteHeader(test.status)
			}))
			defer upstream.Close()
			viewer := viewerServer(t, upstream.URL)
			response, err := viewer.Client().Get(viewer.URL + "/stream/0")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), test.expected) {
				t.Fatalf("received status %d and body %q; expected 502 containing %q", response.StatusCode, body, test.expected)
			}
		})
	}
}

func TestSlowSubscriberDisconnectsWithoutGaps(t *testing.T) {
	chunks := make(chan string)
	upstreamClosed := make(chan struct{})
	var connections atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		defer close(upstreamClosed)
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=camera-test")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case chunk := <-chunks:
				if _, err := io.WriteString(w, chunk); err != nil {
					return
				}
				w.(http.Flusher).Flush()
			}
		}
	}))
	defer upstream.Close()
	streams := &streamManager{streams: make(map[int]*cameraStream)}
	cam := Camera{Name: "slow viewer camera", URL: upstream.URL}
	stream, fast, unsubscribeFast := streams.getOrStart(0, cam)
	defer unsubscribeFast()
	shared, slow, unsubscribeSlow := streams.getOrStart(0, cam)
	defer unsubscribeSlow()
	if stream != shared {
		t.Fatal("viewers did not share the upstream stream")
	}
	select {
	case <-stream.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("camera headers did not arrive")
	}
	var sent, received bytes.Buffer
	for index := 0; index < 16; index++ {
		chunk := fmt.Sprintf("--camera-test\r\nContent-Type: image/jpeg\r\n\r\nimage-%d\r\n", index)
		sent.WriteString(chunk)
		select {
		case chunks <- chunk:
		case <-time.After(5 * time.Second):
			t.Fatal("camera did not accept the next image")
		}
		for received.Len() < sent.Len() {
			select {
			case data, open := <-fast:
				if !open {
					t.Fatal("active viewer was disconnected")
				}
				received.Write(data)
			case <-time.After(5 * time.Second):
				t.Fatal("active viewer did not receive the next image")
			}
		}
	}
	if !bytes.Equal(received.Bytes(), sent.Bytes()) {
		t.Fatalf("active viewer received %q; expected %q", received.Bytes(), sent.Bytes())
	}
	var prefix bytes.Buffer
	for {
		select {
		case chunk, open := <-slow:
			if !open {
				if prefix.Len() == 0 || prefix.Len() >= sent.Len() || !bytes.HasPrefix(sent.Bytes(), prefix.Bytes()) {
					t.Fatalf("slow viewer received %q; expected an uninterrupted prefix of %q", prefix.Bytes(), sent.Bytes())
				}
				unsubscribeFast()
				select {
				case <-upstreamClosed:
				case <-time.After(5 * time.Second):
					t.Fatal("upstream stayed open after its last viewer left")
				}
				if connections.Load() != 1 {
					t.Fatalf("received %d upstream connections; expected one", connections.Load())
				}
				return
			}
			prefix.Write(chunk)
		case <-time.After(5 * time.Second):
			t.Fatal("overflowing viewer queue remained open")
		}
	}
}
