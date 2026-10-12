package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
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
			case data, open := <-fast.chunks:
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
		case chunk, open := <-slow.chunks:
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

func TestEvictionInterruptsBlockedHTTPWrite(t *testing.T) {
	chunks := make(chan []byte)
	upstreamClosed := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamClosed)
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=blocked-reader")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case chunk := <-chunks:
				if _, err := w.Write(chunk); err != nil {
					return
				}
				w.(http.Flusher).Flush()
			}
		}
	}))
	defer upstream.Close()
	oldCameras, oldManager := cameras, manager
	cameras = []Camera{{Name: "blocked viewer camera", URL: upstream.URL}}
	manager = &streamManager{streams: make(map[int]*cameraStream)}
	defer func() { cameras, manager = oldCameras, oldManager }()
	stream, fast, unsubscribeFast := manager.getOrStart(0, cameras[0])
	defer unsubscribeFast()
	handlerDone := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stream/{id}", func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		streamHandler(w, r)
	})
	viewer := httptest.NewUnstartedServer(mux)
	bufferError := make(chan error, 1)
	viewer.Config.ConnContext = func(ctx context.Context, connection net.Conn) context.Context {
		bufferError <- connection.(*net.TCPConn).SetWriteBuffer(1024)
		return ctx
	}
	viewer.Start()
	defer viewer.Close()
	address, err := net.ResolveTCPAddr("tcp", viewer.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTCP("tcp", nil, address)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetReadBuffer(1024); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(connection, "GET /stream/0 HTTP/1.1\r\nHost: localhost\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	if err := <-bufferError; err != nil {
		t.Fatal(err)
	}
	var slow *subscriber
	deadline := time.Now().Add(5 * time.Second)
	for slow == nil && time.Now().Before(deadline) {
		stream.mu.Lock()
		for candidate := range stream.subscribers {
			if candidate != fast {
				slow = candidate
			}
		}
		stream.mu.Unlock()
		if slow == nil {
			time.Sleep(time.Millisecond)
		}
	}
	if slow == nil {
		t.Fatal("TCP viewer did not subscribe")
	}
	frame := []byte("--blocked-reader\r\nContent-Type: image/jpeg\r\n\r\n" + strings.Repeat("x", 64*1024) + "\r\n")
	sendCameraFrame(t, chunks, fast, frame)
	waitForBlockedCameraWrite(t)
	for index := 0; index < 16; index++ {
		select {
		case <-slow.evicted:
			select {
			case <-handlerDone:
			case <-time.After(5 * time.Second):
				t.Fatal("evicted viewer remained blocked writing to its TCP connection")
			}
			sendCameraFrame(t, chunks, fast, frame)
			select {
			case <-upstreamClosed:
				t.Fatal("evicting the slow viewer closed the active viewer's upstream")
			default:
			}
			return
		default:
			sendCameraFrame(t, chunks, fast, frame)
		}
	}
	t.Fatal("TCP viewer's full queue did not trigger eviction")
}

func sendCameraFrame(t *testing.T, chunks chan<- []byte, viewer *subscriber, expected []byte) {
	t.Helper()
	select {
	case chunks <- expected:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream did not accept the next frame")
	}
	var received bytes.Buffer
	for received.Len() < len(expected) {
		select {
		case chunk, open := <-viewer.chunks:
			if !open {
				t.Fatal("active viewer was disconnected")
			}
			received.Write(chunk)
		case <-time.After(5 * time.Second):
			t.Fatal("active viewer did not receive the complete frame")
		}
	}
	if !bytes.Equal(received.Bytes(), expected) {
		t.Fatal("active viewer's frame was corrupted")
	}
}

func waitForBlockedCameraWrite(t *testing.T) {
	t.Helper()
	stacks := make([]byte, 1024*1024)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		length := runtime.Stack(stacks, true)
		for _, stack := range strings.Split(string(stacks[:length]), "\n\n") {
			if strings.Contains(stack, "streamHandler(") && strings.Contains(stack, "net.(*netFD).Write(") {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("camera handler never blocked writing to the non-reading TCP viewer")
}
