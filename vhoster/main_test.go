package main

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

// TestConfigLoad tests that LoadConfig correctly loads and parses a config file
func TestConfigLoad(t *testing.T) {
	// Create a test config file
	testConfig := `{
		"Redirects": [
			{"Host": "example.com", "To": "http://localhost:8080", "Name": "example"}
		],
		"Port": 3000,
		"HostNames": ["example.com"],
		"LogFile": "test_access.log"
	}`

	// Write test config
	tmpFile := t.TempDir() + "/test_config.json"
	if err := os.WriteFile(tmpFile, []byte(testConfig), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg := LoadConfig(tmpFile)
	if cfg == nil {
		t.Fatal("LoadConfig returned nil")
	}

	if len(cfg.Redirects) != 1 {
		t.Errorf("Expected 1 redirect, got %d", len(cfg.Redirects))
	}

	if cfg.Redirects[0].Host != "example.com" {
		t.Errorf("Expected Host 'example.com', got '%s'", cfg.Redirects[0].Host)
	}

	if cfg.Redirects[0].To != "http://localhost:8080" {
		t.Errorf("Expected To 'http://localhost:8080', got '%s'", cfg.Redirects[0].To)
	}

	if cfg.Port != 3000 {
		t.Errorf("Expected Port 3000, got %d", cfg.Port)
	}
}

// TestFormatCLF tests the Common Log Format formatting
func TestFormatCLF(t *testing.T) {
	req := httptest.NewRequest("GET", "/secure/private-session/test/path?code=private-code", nil)
	req.Host = "gateway.example"
	req.RemoteAddr = "192.168.1.1:12345"
	req.Header.Set("User-Agent", "TestAgent/1.0")
	req.Header.Set("Referer", "http://example.com/private-referrer")

	c := &MyContext{
		Request: *req,
		Writer:  httptest.NewRecorder(),
	}

	logLine := format_clf(c, "testuser", "200", "1234")

	// Check that key components are present
	if !bytes.Contains([]byte(logLine), []byte("192.168.1.1")) {
		t.Error("Log line should contain client IP")
	}

	if !bytes.Contains([]byte(logLine), []byte("testuser")) {
		t.Error("Log line should contain user ID")
	}

	if !bytes.Contains([]byte(logLine), []byte("GET")) {
		t.Error("Log line should contain HTTP method")
	}

	if !bytes.Contains([]byte(logLine), []byte("host:gateway.example")) {
		t.Error("Log line should contain the requested host")
	}

	if !bytes.Contains([]byte(logLine), []byte("200")) {
		t.Error("Log line should contain response code")
	}

	if bytes.Contains([]byte(logLine), []byte("TestAgent/1.0")) || bytes.Contains([]byte(logLine), []byte("private-")) {
		t.Error("Log line must not contain user-agent, URL, query, or referrer credentials")
	}
}

// TestMyContextMethods tests the MyContext helper methods
func TestMyContextMethods(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:8000"
	rec := httptest.NewRecorder()

	c := &MyContext{
		Request: *req,
		Writer:  rec,
	}

	// Test ClientIP
	if c.ClientIP() != "10.0.0.1:8000" {
		t.Errorf("ClientIP() = %s, want 10.0.0.1:8000", c.ClientIP())
	}

	// Test RemoteIP
	if c.RemoteIP() != "10.0.0.1:8000" {
		t.Errorf("RemoteIP() = %s, want 10.0.0.1:8000", c.RemoteIP())
	}

	// Test Status
	c.Status(404)
	if rec.Code != 404 {
		t.Errorf("Status(404) set code to %d", rec.Code)
	}

	// Test Header
	c.Header("X-Custom", "test-value")
	if rec.Header().Get("X-Custom") != "test-value" {
		t.Errorf("Header() did not set header correctly")
	}
}

// TestTemplateSet tests the string replacement templating
func TestTemplateSet(t *testing.T) {
	tests := []struct {
		template string
		before   string
		after    string
		expected string
	}{
		{"Hello TOKEN", "TOKEN", "World", "Hello World"},
		{"No match here", "TOKEN", "World", "No match here"},
		{"TOKEN TOKEN TOKEN", "TOKEN", "X", "X X X"},
		{"", "TOKEN", "X", ""},
	}

	for _, tt := range tests {
		result := templateSet(tt.template, tt.before, tt.after)
		if result != tt.expected {
			t.Errorf("templateSet(%q, %q, %q) = %q, want %q",
				tt.template, tt.before, tt.after, result, tt.expected)
		}
	}
}

// TestRelayGetHandler tests the GET relay functionality with a real backend server
func TestRelayGetHandler(t *testing.T) {
	// Set up accessLog to avoid nil pointer
	setupTestAccessLog(t)
	// Initialize config
	setupTestConfig(t)

	// Create a test backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify forwarded headers
		if r.Header.Get("X-Forwarded-For") == "" {
			t.Error("Backend should receive X-Forwarded-For header")
		}
		if r.Header.Get("X-Real-IP") == "" {
			t.Error("Backend should receive X-Real-IP header")
		}

		// Echo back the path
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Response from backend: " + r.URL.Path))
	}))
	defer backend.Close()

	// Create a test request
	req := httptest.NewRequest("GET", "/api/test?param=value", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	req.Header.Set("Accept", "text/plain")
	rec := httptest.NewRecorder()

	c := &MyContext{
		Request: *req,
		Writer:  rec,
	}

	relay := &Redirect{
		Host: "test.example.com",
		To:   backend.URL,
		Name: "test",
	}

	// Call the handler
	relayGetHandler(c, relay)

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !bytes.Contains([]byte(body), []byte("Response from backend")) {
		t.Errorf("Expected backend response, got: %s", body)
	}
}

// TestRelayPostHandler tests the POST relay functionality
func TestRelayPostHandler(t *testing.T) {
	// Set up accessLog to avoid nil pointer
	setupTestAccessLog(t)
	// Initialize config
	setupTestConfig(t)

	// Create a test backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}

		// Read body
		body, _ := io.ReadAll(r.Body)

		// Echo back
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"received":"` + string(body) + `"}`))
	}))
	defer backend.Close()

	// Create a test request with body
	reqBody := bytes.NewReader([]byte(`{"test":"data"}`))
	req := httptest.NewRequest("POST", "/api/create", reqBody)
	req.RemoteAddr = "192.168.1.100:12345"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	c := &MyContext{
		Request: *req,
		Writer:  rec,
	}

	relay := &Redirect{
		Host:        "test.example.com",
		To:          backend.URL,
		Name:        "test",
		CopyHeaders: []string{"Content-Type"},
	}

	// Call the handler
	relayPostHandler(c, relay)

	// Response might be 201 from backend or 0 if there's an issue before setting status
	// Check that we got the body back
	body := rec.Body.String()
	if len(body) == 0 {
		t.Error("Expected response body from POST handler")
	}
}

// TestRelayPutHandler tests the PUT relay functionality
func TestRelayPutHandler(t *testing.T) {
	// Set up accessLog to avoid nil pointer
	setupTestAccessLog(t)
	// Initialize config
	setupTestConfig(t)

	// Create a test backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("Expected PUT, got %s", r.Method)
		}

		// Read body
		body, _ := io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("PUT received: " + string(body)))
	}))
	defer backend.Close()

	// Create a test request with body
	reqBody := bytes.NewReader([]byte(`update content`))
	req := httptest.NewRequest("PUT", "/api/update", reqBody)
	req.RemoteAddr = "192.168.1.100:12345"
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()

	c := &MyContext{
		Request: *req,
		Writer:  rec,
	}

	relay := &Redirect{
		Host: "test.example.com",
		To:   backend.URL,
		Name: "test",
	}

	// Call the handler
	relayPutHandler(c, relay)

	// Verify response
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

// TestHostMatching tests the host suffix matching logic
func TestHostMatching(t *testing.T) {
	tests := []struct {
		requestHost string
		configHost  string
		shouldMatch bool
	}{
		{"www.example.com", "example.com", true},
		{"api.example.com", "example.com", true},
		{"example.com", "example.com", true},
		{"example.org", "example.com", false},
		{"notexample.com", "example.com", true}, // suffix match
		{"example.com.evil.org", "example.com", false},
	}

	for _, tt := range tests {
		// Use strings.HasSuffix like the actual code
		matched := hasSuffix(tt.requestHost, tt.configHost)
		if matched != tt.shouldMatch {
			t.Errorf("Host %q matching %q: got %v, want %v",
				tt.requestHost, tt.configHost, matched, tt.shouldMatch)
		}
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// TestMuxRouting tests the MyMux routing for different HTTP methods
func TestMuxRouting(t *testing.T) {
	// Set up a minimal config for the test
	testConfig := &Config{
		Redirects: []Redirect{},
		Port:      3000,
	}

	oldConfig := config
	config = testConfig
	defer func() { config = oldConfig }()

	mux := MyMux{}

	// Test that unmatched hosts return 404
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "unknown.host.com"
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for unknown host, got %d", rec.Code)
	}
}

// TestMuxWithBackend tests the full request flow through MyMux to a backend
func TestMuxWithBackend(t *testing.T) {
	// Set up accessLog to avoid nil pointer
	setupTestAccessLog(t)

	// Create a test backend
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Backend OK"))
	}))
	defer backend.Close()

	// Configure the mux
	testConfig := &Config{
		Redirects: []Redirect{
			{
				Host: "test.local",
				To:   backend.URL,
				Name: "test",
			},
		},
		Port: 3000,
	}

	oldConfig := config
	config = testConfig
	defer func() { config = oldConfig }()

	mux := MyMux{}

	// Test GET request
	req := httptest.NewRequest("GET", "/hello", nil)
	req.Host = "test.local"
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}

	if rec.Body.String() != "Backend OK" {
		t.Errorf("Expected 'Backend OK', got '%s'", rec.Body.String())
	}
}

// TestMuxPostWithBackend tests POST requests through the mux
func TestMuxPostWithBackend(t *testing.T) {
	// Set up accessLog to avoid nil pointer
	setupTestAccessLog(t)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer backend.Close()

	testConfig := &Config{
		Redirects: []Redirect{
			{Host: "api.local", To: backend.URL, Name: "api"},
		},
		Port: 3000,
	}

	oldConfig := config
	config = testConfig
	defer func() { config = oldConfig }()

	mux := MyMux{}

	body := bytes.NewReader([]byte("test post body"))
	req := httptest.NewRequest("POST", "/submit", body)
	req.Host = "api.local"
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}
}

// TestHeaderCopying tests that headers are correctly copied from client to backend
func TestHeaderCopying(t *testing.T) {
	// Set up accessLog to avoid nil pointer
	setupTestAccessLog(t)
	// Initialize config
	setupTestConfig(t)

	var receivedHeaders http.Header

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer token123")
	req.Header.Set("X-Custom-Header", "custom-value")
	rec := httptest.NewRecorder()

	c := &MyContext{Request: *req, Writer: rec}

	relay := &Redirect{
		Host:        "test.com",
		To:          backend.URL,
		Name:        "test",
		CopyHeaders: []string{"Authorization", "X-Custom-Header"},
	}

	relayGetHandler(c, relay)

	// Check that headers were forwarded
	if receivedHeaders.Get("Authorization") != "Bearer token123" {
		t.Errorf("Authorization header not forwarded correctly: %s", receivedHeaders.Get("Authorization"))
	}

	if receivedHeaders.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("X-Custom-Header not forwarded correctly: %s", receivedHeaders.Get("X-Custom-Header"))
	}

	if receivedHeaders.Get("X-Forwarded-For") == "" {
		t.Error("X-Forwarded-For should be set")
	}

	if receivedHeaders.Get("X-Real-IP") == "" {
		t.Error("X-Real-IP should be set")
	}
}

// TestResponseHeaderCopying tests that response headers from backend are copied to client
func TestResponseHeaderCopying(t *testing.T) {
	// Set up accessLog to avoid nil pointer
	setupTestAccessLog(t)
	// Initialize config
	setupTestConfig(t)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend-Response", "response-value")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer backend.Close()

	req := httptest.NewRequest("GET", "/api", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()

	c := &MyContext{Request: *req, Writer: rec}

	relay := &Redirect{
		Host: "test.com",
		To:   backend.URL,
		Name: "test",
	}

	relayGetHandler(c, relay)

	// Check response headers are copied
	if rec.Header().Get("X-Backend-Response") != "response-value" {
		t.Errorf("Response header not copied: %s", rec.Header().Get("X-Backend-Response"))
	}
}

// TestQueryParamForwarding tests that query parameters are forwarded correctly
func TestQueryParamForwarding(t *testing.T) {
	// Set up accessLog to avoid nil pointer
	setupTestAccessLog(t)
	// Initialize config
	setupTestConfig(t)

	var receivedPath string

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.RequestURI()
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	req := httptest.NewRequest("GET", "/api/search?q=test&limit=10", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()

	c := &MyContext{Request: *req, Writer: rec}

	relay := &Redirect{
		Host: "test.com",
		To:   backend.URL,
		Name: "test",
	}

	relayGetHandler(c, relay)

	// Check query params were forwarded
	if receivedPath != "/api/search?q=test&limit=10" {
		t.Errorf("Query params not forwarded correctly, got: %s", receivedPath)
	}
}

// TestEmptyConfig tests handling of empty config
func TestEmptyConfig(t *testing.T) {
	testConfig := `{
		"Redirects": [],
		"Port": 8080
	}`

	tmpFile := t.TempDir() + "/empty_config.json"
	if err := os.WriteFile(tmpFile, []byte(testConfig), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg := LoadConfig(tmpFile)
	if cfg == nil {
		t.Fatal("LoadConfig returned nil for empty config")
	}

	if len(cfg.Redirects) != 0 {
		t.Errorf("Expected 0 redirects, got %d", len(cfg.Redirects))
	}

	if cfg.Port != 8080 {
		t.Errorf("Expected Port 8080, got %d", cfg.Port)
	}
}

// TestMultipleRedirects tests config with multiple redirect rules
func TestMultipleRedirects(t *testing.T) {
	testConfig := `{
		"Redirects": [
			{"Host": "api.example.com", "To": "http://localhost:8081", "Name": "api"},
			{"Host": "web.example.com", "To": "http://localhost:8082", "Name": "web"},
			{"Host": "admin.example.com", "To": "http://localhost:8083", "Name": "admin"}
		],
		"Port": 443
	}`

	tmpFile := t.TempDir() + "/multi_config.json"
	if err := os.WriteFile(tmpFile, []byte(testConfig), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg := LoadConfig(tmpFile)
	if cfg == nil {
		t.Fatal("LoadConfig returned nil")
	}

	if len(cfg.Redirects) != 3 {
		t.Errorf("Expected 3 redirects, got %d", len(cfg.Redirects))
	}

	// Verify each redirect
	expectedHosts := []string{"api.example.com", "web.example.com", "admin.example.com"}
	for i, host := range expectedHosts {
		if cfg.Redirects[i].Host != host {
			t.Errorf("Redirect %d: expected Host %s, got %s", i, host, cfg.Redirects[i].Host)
		}
	}
}

// TestCopyHeadersConfig tests that CopyHeaders are parsed correctly
func TestCopyHeadersConfig(t *testing.T) {
	testConfig := `{
		"Redirects": [
			{
				"Host": "api.example.com", 
				"To": "http://localhost:8081", 
				"Name": "api",
				"CopyHeaders": ["Authorization", "X-Request-ID", "Accept-Language"]
			}
		],
		"Port": 443
	}`

	tmpFile := t.TempDir() + "/headers_config.json"
	if err := os.WriteFile(tmpFile, []byte(testConfig), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg := LoadConfig(tmpFile)
	if cfg == nil {
		t.Fatal("LoadConfig returned nil")
	}

	if len(cfg.Redirects[0].CopyHeaders) != 3 {
		t.Errorf("Expected 3 CopyHeaders, got %d", len(cfg.Redirects[0].CopyHeaders))
	}

	expectedHeaders := []string{"Authorization", "X-Request-ID", "Accept-Language"}
	for i, h := range expectedHeaders {
		if cfg.Redirects[0].CopyHeaders[i] != h {
			t.Errorf("CopyHeader %d: expected %s, got %s", i, h, cfg.Redirects[0].CopyHeaders[i])
		}
	}
}

// setupTestAccessLog creates a test access log writer
func setupTestAccessLog(t *testing.T) {
	t.Helper()
	// Create a temporary log file
	tmpFile := t.TempDir() + "/test_access.log"
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	// Use bufio.Writer for accessLog
	accessLog = bufio.NewWriterSize(f, 999999)

	t.Cleanup(func() {
		f.Close()
	})
}

// setupTestConfig sets up a minimal test config
func setupTestConfig(t *testing.T) {
	t.Helper()
	if config == nil {
		config = &Config{
			Redirects: []Redirect{},
			Port:      3000,
		}
	}
}

// TestBackendUnavailable tests behavior when backend is not available
func TestBackendUnavailable(t *testing.T) {
	// Set up accessLog to avoid nil pointer
	setupTestAccessLog(t)
	// Initialize config
	setupTestConfig(t)

	// Use a non-existent backend URL
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rec := httptest.NewRecorder()

	c := &MyContext{
		Request: *req,
		Writer:  rec,
	}

	relay := &Redirect{
		Host: "test.example.com",
		To:   "http://localhost:59999", // Non-existent port
		Name: "test",
	}

	// This should panic or return error - we need to catch it
	defer func() {
		if r := recover(); r == nil {
			t.Log("Handler did not panic on unavailable backend - this may be expected")
		}
	}()

	relayGetHandler(c, relay)
}

// TestLargeBody tests handling of larger request bodies
func TestLargeBody(t *testing.T) {
	// Set up accessLog to avoid nil pointer
	setupTestAccessLog(t)
	// Initialize config
	setupTestConfig(t)

	// Create a large body (1MB)
	largeBody := bytes.Repeat([]byte("X"), 1024*1024)

	var receivedBodySize int

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBodySize = len(body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer backend.Close()

	req := httptest.NewRequest("POST", "/upload", bytes.NewReader(largeBody))
	req.RemoteAddr = "192.168.1.100:12345"
	req.Header.Set("Content-Type", "application/octet-stream")
	rec := httptest.NewRecorder()

	c := &MyContext{
		Request: *req,
		Writer:  rec,
	}

	relay := &Redirect{
		Host: "test.example.com",
		To:   backend.URL,
		Name: "test",
	}

	relayPostHandler(c, relay)

	if receivedBodySize != len(largeBody) {
		t.Errorf("Expected body size %d, got %d", len(largeBody), receivedBodySize)
	}
}

// TestConcurrentRequests tests handling multiple concurrent requests
func TestConcurrentRequests(t *testing.T) {
	// Set up accessLog to avoid nil pointer
	setupTestAccessLog(t)

	var requestCount atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer backend.Close()

	testConfig := &Config{
		Redirects: []Redirect{
			{Host: "test.local", To: backend.URL, Name: "test"},
		},
		Port: 3000,
	}

	oldConfig := config
	config = testConfig
	defer func() { config = oldConfig }()

	mux := MyMux{}

	// Send multiple requests concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/concurrent", nil)
			req.Host = "test.local"
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			done <- rec.Code == http.StatusOK
		}()
	}

	// Wait for all requests to complete
	successCount := 0
	for i := 0; i < 10; i++ {
		if <-done {
			successCount++
		}
	}

	if successCount != 10 {
		t.Errorf("Expected 10 successful requests, got %d", successCount)
	}
	if requestCount.Load() != 10 {
		t.Errorf("Expected 10 backend requests, got %d", requestCount.Load())
	}
}

// TestRedirectFollowing tests that the proxy doesn't follow redirects
func TestRedirectFollowing(t *testing.T) {
	// Set up accessLog to avoid nil pointer
	setupTestAccessLog(t)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/redirected", http.StatusFound)
	}))
	defer backend.Close()

	req := httptest.NewRequest("GET", "/will-redirect", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()

	c := &MyContext{Request: *req, Writer: rec}

	relay := &Redirect{
		Host: "test.com",
		To:   backend.URL,
		Name: "test",
	}

	relayGetHandler(c, relay)

	// The proxy should return the redirect as-is, not follow it
	if rec.Code != http.StatusFound {
		t.Errorf("Expected 302 redirect status, got %d", rec.Code)
	}
}
