package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
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

// subscriber receives MJPEG chunks and an eviction signal when it falls behind.
type subscriber struct {
	chunks  chan []byte
	evicted chan struct{}
}

// cameraStream manages a single upstream camera connection and fans it out to
// all active subscribers. It closes the upstream when the last subscriber leaves.
type cameraStream struct {
	mu          sync.Mutex
	subscribers map[*subscriber]struct{}
	cancel      func() // closes the upstream connection
	ready       chan struct{}
	contentType string
	startupErr  error
	closed      bool
}

func newCameraStream() *cameraStream {
	return &cameraStream{
		subscribers: make(map[*subscriber]struct{}),
		ready:       make(chan struct{}),
	}
}

// subscribe adds a subscriber and returns it plus an unsubscribe func.
func (s *cameraStream) subscribe() (*subscriber, func(), bool) {
	viewer := &subscriber{chunks: make(chan []byte, 8), evicted: make(chan struct{})}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, nil, false
	}
	s.subscribers[viewer] = struct{}{}
	s.mu.Unlock()
	return viewer, func() {
		s.mu.Lock()
		delete(s.subscribers, viewer)
		empty := len(s.subscribers) == 0
		if empty {
			s.closed = true
		}
		cancel := s.cancel
		s.mu.Unlock()
		if empty && cancel != nil {
			cancel()
		}
	}, true
}

// broadcast sends a chunk to all subscribers; drops slow ones.
func (s *cameraStream) broadcast(chunk []byte) {
	cp := make([]byte, len(chunk))
	copy(cp, chunk)
	s.mu.Lock()
	for viewer := range s.subscribers {
		select {
		case viewer.chunks <- cp:
		default:
			close(viewer.evicted)
			close(viewer.chunks)
			delete(s.subscribers, viewer)
		}
	}
	empty := len(s.subscribers) == 0
	if empty {
		s.closed = true
	}
	cancel := s.cancel
	s.mu.Unlock()
	if empty && cancel != nil {
		cancel()
	}
}

// closeAll closes all subscriber channels so readers unblock.
func (s *cameraStream) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for viewer := range s.subscribers {
		close(viewer.chunks)
		delete(s.subscribers, viewer)
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

// getOrStart subscribes to the active cameraStream for camIdx, starting the
// upstream pump after registration if there isn't one already.
func (m *streamManager) getOrStart(camIdx int, cam Camera) (*cameraStream, *subscriber, func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.streams[camIdx]; ok {
		if ch, unsub, active := s.subscribe(); active {
			return s, ch, unsub
		}
	}

	s := newCameraStream()
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	ch, unsub, _ := s.subscribe()
	m.streams[camIdx] = s

	go func() {
		defer cancel()
		defer func() {
			m.mu.Lock()
			if m.streams[camIdx] == s {
				delete(m.streams, camIdx)
			}
			m.mu.Unlock()
			s.closeAll()
			logCameraClosed(camIdx, cam.Name)
		}()

		logCameraOpening(camIdx, cam.Name)
		resp, err := openCamera(ctx, cam)
		s.startupErr = err
		if err == nil {
			s.contentType = resp.Header.Get("Content-Type")
		}
		close(s.ready)
		if err != nil {
			logCameraOpenFailed(camIdx, cam.Name, err)
			return
		}
		defer resp.Body.Close()

		buf := make([]byte, 32*1024)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				s.broadcast(buf[:n])
			}
			if readErr != nil {
				if readErr != io.EOF && ctx.Err() == nil {
					logCameraReadFailed(camIdx, cam.Name, readErr)
				}
				return
			}
		}
	}()

	return s, ch, unsub
}

func openCamera(ctx context.Context, cam Camera) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cam.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid camera URL: %w", cameraErrorCause(err))
	}
	resp, err := cameraClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("camera connection failed: %w", cameraErrorCause(err))
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("camera returned HTTP %d; expected HTTP 200", resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("camera returned invalid Content-Type %q; expected multipart/x-mixed-replace with a boundary: %w", contentType, err)
	}
	if mediaType != "multipart/x-mixed-replace" || parameters["boundary"] == "" {
		resp.Body.Close()
		return nil, fmt.Errorf("camera returned Content-Type %q; expected multipart/x-mixed-replace with a boundary", contentType)
	}
	return resp, nil
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
			return nil, fmt.Errorf("loadCameras: camera %d (%q) has invalid URL: %w", i, cam.Name, cameraErrorCause(parseErr))
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
		logCameraPageRequest(id)
		handlerFunc(c, id, baseUrl, topUrl)
	}
}

// makeAuthedStream wraps a plain http.HandlerFunc with authentigate header logging.
func makeAuthedStream(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("authentigate-id")
		logCameraStreamRequest(id)
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
	stream, viewer, unsub := manager.getOrStart(camIdx, cam)
	stopWatching := watchViewer(w, r.Context(), viewer.evicted, camIdx, cam.Name)
	defer func() {
		stopWatching()
		unsub()
	}()

	flusher, canFlush := w.(http.Flusher)
	clientCtx := r.Context()

	select {
	case <-clientCtx.Done():
		return
	case <-stream.ready:
	}
	if stream.startupErr != nil {
		http.Error(w, fmt.Sprintf("camera %d (%q): %v", camIdx, cam.Name, stream.startupErr), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", stream.contentType)
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)

	for {
		select {
		case <-clientCtx.Done():
			return
		case <-viewer.evicted:
			return
		case chunk, ok := <-viewer.chunks:
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

func watchViewer(w http.ResponseWriter, ctx context.Context, evicted <-chan struct{}, index int, name string) func() {
	stop := make(chan struct{})
	stopped := make(chan struct{})
	controller := http.NewResponseController(w)
	go func() {
		defer close(stopped)
		select {
		case <-stop:
			select {
			case <-evicted:
			default:
				return
			}
		case <-ctx.Done():
		case <-evicted:
		}
		if err := controller.SetWriteDeadline(time.Now()); err != nil {
			logCameraViewerDeadlineFailed(index, name, err)
		}
	}()
	return func() {
		close(stop)
		<-stopped
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
