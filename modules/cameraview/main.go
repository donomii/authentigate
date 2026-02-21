package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"
)

// Camera holds the configuration for a single camera stream.
type Camera struct {
	Name string `json:"Name"`
	URL  string `json:"URL"`
}

// Config holds all cameras loaded from cameras.json.
type Config struct {
	Cameras []Camera `json:"Cameras"`
}

// Context wraps an HTTP request/response pair for handler convenience.
type Context struct {
	Writer  http.ResponseWriter
	Request *http.Request
}

func (c *Context) Query(key string) string {
	return c.Request.URL.Query().Get(key)
}

var cameras []Camera

// cameraClient has connection-establishment and response-header timeouts but no
// overall timeout, since MJPEG streams are intentionally long-lived.
var cameraClient = &http.Client{
	Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		ResponseHeaderTimeout: 15 * time.Second,
	},
}

// subscriber is a channel that receives chunks of MJPEG data.
type subscriber chan []byte

// cameraStream manages a single upstream camera connection and fans it out to
// all active subscribers. It closes the upstream when the last subscriber leaves.
type cameraStream struct {
	mu          sync.Mutex
	subscribers map[subscriber]struct{}
	cancel      func() // closes the upstream connection
}

func newCameraStream() *cameraStream {
	return &cameraStream{
		subscribers: make(map[subscriber]struct{}),
	}
}

// subscribe adds a subscriber and returns its channel plus an unsubscribe func.
func (s *cameraStream) subscribe() (subscriber, func()) {
	ch := make(subscriber, 8)
	s.mu.Lock()
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subscribers, ch)
		empty := len(s.subscribers) == 0
		cancel := s.cancel
		s.mu.Unlock()
		if empty && cancel != nil {
			cancel()
		}
	}
}

// broadcast sends a chunk to all subscribers; drops slow ones.
func (s *cameraStream) broadcast(chunk []byte) {
	cp := make([]byte, len(chunk))
	copy(cp, chunk)
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subscribers {
		select {
		case ch <- cp:
		default:
		}
	}
}

// closeAll closes all subscriber channels so readers unblock.
func (s *cameraStream) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subscribers {
		close(ch)
		delete(s.subscribers, ch)
	}
}

// streamManager holds one cameraStream per camera index, creating on demand.
type streamManager struct {
	mu      sync.Mutex
	streams map[int]*cameraStream
}

var manager = &streamManager{
	streams: make(map[int]*cameraStream),
}

// getOrStart returns the active cameraStream for camIdx, starting the upstream
// pump if there isn't one already.
func (m *streamManager) getOrStart(camIdx int, cam Camera) *cameraStream {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.streams[camIdx]; ok {
		return s
	}

	s := newCameraStream()
	m.streams[camIdx] = s

	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.streams, camIdx)
			m.mu.Unlock()
			s.closeAll()
			log.Printf("streamManager: upstream closed for camera %d (%q)", camIdx, cam.Name)
		}()

		log.Printf("streamManager: opening upstream for camera %d (%q)", camIdx, cam.Name)

		ctx, cancel := context.WithCancel(context.Background())
		s.mu.Lock()
		s.cancel = cancel
		s.mu.Unlock()

		req, err := http.NewRequestWithContext(ctx, "GET", cam.URL, nil)
		if err != nil {
			cancel()
			log.Printf("streamManager: bad URL for camera %d (%q): %v", camIdx, cam.Name, err)
			return
		}

		resp, err := cameraClient.Do(req)
		if err != nil {
			log.Printf("streamManager: could not connect to camera %d (%q): %v", camIdx, cam.Name, err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Printf("streamManager: camera %d (%q) returned status %d", camIdx, cam.Name, resp.StatusCode)
			return
		}

		buf := make([]byte, 32*1024)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				s.broadcast(buf[:n])
			}
			if readErr != nil {
				if readErr != io.EOF {
					log.Printf("streamManager: read ended for camera %d (%q): %v", camIdx, cam.Name, readErr)
				}
				return
			}
		}
	}()

	return s
}


func loadCameras(filename string) ([]Camera, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("loadCameras: could not read %q: %w", filename, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("loadCameras: could not parse %q: %w", filename, err)
	}
	for i, cam := range cfg.Cameras {
		if cam.URL == "" {
			return nil, fmt.Errorf("loadCameras: camera %d (%q) has empty URL", i, cam.Name)
		}
		u, parseErr := url.Parse(cam.URL)
		if parseErr != nil {
			return nil, fmt.Errorf("loadCameras: camera %d (%q) has invalid URL %q: %w", i, cam.Name, cam.URL, parseErr)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("loadCameras: camera %d (%q) has disallowed scheme %q (only http/https allowed)", i, cam.Name, u.Scheme)
		}
	}
	return cfg.Cameras, nil
}

// makeAuthed wraps a handler so that authentigate injects id, baseUrl, and topUrl.
func makeAuthed(handlerFunc func(*Context, string, string, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := &Context{Writer: w, Request: r}
		id := r.Header.Get("authentigate-id")
		baseUrl := r.Header.Get("authentigate-base-url")
		topUrl := r.Header.Get("authentigate-top-url")
		log.Printf("cameraview: user=%q baseUrl=%q", id, baseUrl)
		handlerFunc(c, id, baseUrl, topUrl)
	}
}

// makeAuthedStream wraps a plain http.HandlerFunc with authentigate header logging.
func makeAuthedStream(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("authentigate-id")
		baseUrl := r.Header.Get("authentigate-base-url")
		log.Printf("cameraview stream: user=%q baseUrl=%q", id, baseUrl)
		h(w, r)
	}
}

// indexHandler serves the camera grid page.
func indexHandler(c *Context, id, baseUrl, topUrl string) {
	data, err := os.ReadFile("files/index.html")
	if err != nil {
		log.Printf("indexHandler: could not read files/index.html: %v", err)
		http.Error(c.Writer, "internal error: could not load page", http.StatusInternalServerError)
		return
	}
	c.Writer.Header().Set("Content-Type", "text/html")
	c.Writer.Write(data)
}

// listHandler returns the camera list as JSON.
func listHandler(c *Context, id, baseUrl, topUrl string) {
	type cameraInfo struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	var list []cameraInfo
	for i, cam := range cameras {
		list = append(list, cameraInfo{ID: i, Name: cam.Name})
	}
	data, err := json.Marshal(list)
	if err != nil {
		log.Printf("listHandler: marshal error: %v", err)
		http.Error(c.Writer, "internal error", http.StatusInternalServerError)
		return
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.Write(data)
}

// streamHandler subscribes to the shared upstream for a camera and relays
// frames to the HTTP client. The upstream is closed when the last client leaves.
func streamHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	camIdx, err := strconv.Atoi(idStr)
	if err != nil || camIdx < 0 || camIdx >= len(cameras) {
		log.Printf("streamHandler: invalid camera id %q (have %d cameras)", idStr, len(cameras))
		http.Error(w, "invalid camera id", http.StatusBadRequest)
		return
	}

	cam := cameras[camIdx]
	stream := manager.getOrStart(camIdx, cam)

	ch, unsub := stream.subscribe()
	defer unsub()

	flusher, canFlush := w.(http.Flusher)
	clientCtx := r.Context()

	// We don't know the Content-Type until the upstream sends headers, but
	// since the upstream pump doesn't expose headers we set a sensible default.
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)

	for {
		select {
		case <-clientCtx.Done():
			return
		case chunk, ok := <-ch:
			if !ok {
				// upstream closed
				return
			}
			if _, writeErr := w.Write(chunk); writeErr != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
	}
}

func main() {
	var err error
	cameras, err = loadCameras("cameras.json")
	if err != nil {
		log.Fatalf("cameraview: failed to load cameras.json: %v", err)
	}
	log.Printf("cameraview: loaded %d camera(s)", len(cameras))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /cameraview/", makeAuthed(indexHandler))
	mux.HandleFunc("GET /cameraview/list", makeAuthed(listHandler))
	mux.HandleFunc("GET /cameraview/stream/{id}", makeAuthedStream(streamHandler))

	addr := "127.0.0.1:8094"
	fmt.Printf("Starting cameraview module on %s...\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
