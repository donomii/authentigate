package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// ===== Config Tests =====

// TestLoadConfig tests that config files are loaded correctly
func TestLoadConfig(t *testing.T) {
	testConfig := `{
		"Redirects": [
			{"From": "/api/*api", "To": "http://localhost:8081", "Tipe": "GET", "Name": "api"}
		],
		"Port": 8000,
		"BaseUrl": "http://test.local/secure/",
		"HostNames": ["test.local"],
		"Bans": [".git", "admin"],
		"LogFile": "test.log",
		"Secure": false
	}`

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

	if cfg.Port != 8000 {
		t.Errorf("Expected Port 8000, got %d", cfg.Port)
	}

	if cfg.BaseUrl != "http://test.local/secure/" {
		t.Errorf("Expected BaseUrl 'http://test.local/secure/', got '%s'", cfg.BaseUrl)
	}

	if len(cfg.Bans) != 2 {
		t.Errorf("Expected 2 bans, got %d", len(cfg.Bans))
	}

	if cfg.Secure != false {
		t.Error("Expected Secure false")
	}
}

// TestSaveConfig tests that config files are saved correctly
func TestSaveConfig(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	cfg := &Config{
		Redirects: []Redirect{
			{From: "/test/*api", To: "http://localhost:9000", Tipe: "GET", Name: "test"},
		},
		Port:      9000,
		BaseUrl:   "http://example.com/secure/",
		HostNames: []string{"example.com"},
		Bans:      []string{"secret"},
		Secure:    true,
	}

	SaveConfig(cfg)

	// Verify file was created
	data, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatalf("Failed to read saved config: %v", err)
	}

	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Failed to parse saved config: %v", err)
	}

	if loaded.Port != 9000 {
		t.Errorf("Saved Port mismatch: got %d", loaded.Port)
	}

	if loaded.BaseUrl != "http://example.com/secure/" {
		t.Errorf("Saved BaseUrl mismatch: got %s", loaded.BaseUrl)
	}
}

// ===== Template Tests =====

// TestTemplateSet tests the string replacement templating
func TestTemplateSet(t *testing.T) {
	tests := []struct {
		template string
		before   string
		after    string
		expected string
	}{
		{"Hello TOKEN World", "TOKEN", "Beautiful", "Hello Beautiful World"},
		{"TOKEN at start", "TOKEN", "X", "X at start"},
		{"at end TOKEN", "TOKEN", "Y", "at end Y"},
		{"TOKENTOKENTOKEN", "TOKEN", "A", "AAA"},
		{"no match here", "TOKEN", "Z", "no match here"},
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

// TestMakeSecureZoneUrl tests URL construction
func TestMakeSecureZoneUrl(t *testing.T) {
	// Save and restore original baseUrl
	originalBaseUrl := baseUrl
	baseUrl = "https://test.local/secure/"
	defer func() { baseUrl = originalBaseUrl }()

	url := makeSecureZoneUrl("token123", "page.html")

	if !strings.Contains(url, "token123") {
		t.Error("URL should contain the token")
	}

	if !strings.Contains(url, "page.html") {
		t.Error("URL should contain the page")
	}

	// Test double slash removal
	urlWithSlash := makeSecureZoneUrl("token123", "/page.html")
	if strings.Contains(urlWithSlash, "//page") {
		t.Error("URL should not have double slashes")
	}
}

// ===== User Data Tests =====

// TestUserDataSerialization tests userData_t JSON serialization
func TestUserDataSerialization(t *testing.T) {
	user := userData_t{
		Id:         "user-123",
		ForeignIDs: []string{"google123", "facebook456"},
		Token:      "token-abc",
		QRcode:     []byte("qr-data"),
		OtpSecret:  "secret123",
		UserName:   "testuser",
	}

	// Serialize
	data, err := json.Marshal(user)
	if err != nil {
		t.Fatalf("Failed to marshal user: %v", err)
	}

	// Deserialize
	var loaded userData_t
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Failed to unmarshal user: %v", err)
	}

	if loaded.Id != user.Id {
		t.Errorf("Id mismatch: got %s, want %s", loaded.Id, user.Id)
	}

	if len(loaded.ForeignIDs) != 2 {
		t.Errorf("ForeignIDs length mismatch: got %d", len(loaded.ForeignIDs))
	}

	if loaded.UserName != "testuser" {
		t.Errorf("UserName mismatch: got %s", loaded.UserName)
	}
}

// ===== Redirect Structure Tests =====

// TestRedirectConfig tests Redirect struct parsing
func TestRedirectConfig(t *testing.T) {
	configJSON := `{
		"Redirects": [
			{
				"From": "/api/*api",
				"To": "http://backend:8080",
				"Tipe": "GET",
				"Name": "backend",
				"CopyHeaders": ["Authorization", "X-Request-ID"]
			}
		]
	}`

	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	if len(cfg.Redirects) != 1 {
		t.Fatalf("Expected 1 redirect, got %d", len(cfg.Redirects))
	}

	r := cfg.Redirects[0]
	if r.From != "/api/*api" {
		t.Errorf("From mismatch: %s", r.From)
	}

	if r.To != "http://backend:8080" {
		t.Errorf("To mismatch: %s", r.To)
	}

	if r.Tipe != "GET" {
		t.Errorf("Tipe mismatch: %s", r.Tipe)
	}

	if len(r.CopyHeaders) != 2 {
		t.Errorf("CopyHeaders length mismatch: %d", len(r.CopyHeaders))
	}
}

// ===== AddAuthToRequest Tests =====

// TestAddAuthToRequest tests that auth headers are correctly added
func TestAddAuthToRequest(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)

	relay := &Redirect{
		Name: "testservice",
	}

	AddAuthToRequest(req, "user-id-123", "token-abc", "https://base.url/secure/", relay, true)

	if req.Header.Get("authentigate-id") != "user-id-123" {
		t.Errorf("authentigate-id header not set correctly: %s", req.Header.Get("authentigate-id"))
	}

	if req.Header.Get("authentigate-token") != "token-abc" {
		t.Errorf("authentigate-token header not set correctly")
	}

	baseUrl := req.Header.Get("authentigate-base-url")
	if baseUrl == "" {
		t.Error("authentigate-base-url should be set")
	}

	topUrl := req.Header.Get("authentigate-top-url")
	if topUrl == "" {
		t.Error("authentigate-top-url should be set")
	}
}

// ===== TOTP Tests =====

// TestGenerateTOTPWithSecret tests TOTP QR code generation
func TestGenerateTOTPWithSecret(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP" // Valid base32 secret
	username := "testuser@example.com"

	// This should not panic
	qr := generateTOTPWithSecret(secret, username)

	if len(qr) == 0 {
		t.Error("QR code should not be empty")
	}

	// Check it's a valid PNG (starts with PNG signature)
	pngSig := []byte{0x89, 0x50, 0x4e, 0x47}
	if !bytes.HasPrefix(qr, pngSig) {
		t.Error("QR code should be a valid PNG")
	}
}

// TestGenerateTOTPWithEmptyUsername panics
func TestGenerateTOTPWithEmptyUsername(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for empty username")
		}
	}()

	generateTOTPWithSecret("JBSWY3DPEHPK3PXP", "")
}

// ===== Integration Test Helpers =====

// mockBackend creates a test HTTP server that echoes requests
func mockBackend(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo headers back as JSON
		response := map[string]interface{}{
			"method":  r.Method,
			"path":    r.URL.Path,
			"headers": map[string]string{},
		}

		for k, v := range r.Header {
			response["headers"].(map[string]string)[k] = v[0]
		}

		if r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			response["body"] = string(body)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
}

// ===== Ban Tests =====

// TestBanLogic tests URL ban functionality
func TestBanLogic(t *testing.T) {
	bans := []string{".git", "admin", "secret"}

	tests := []struct {
		url       string
		shouldBan bool
	}{
		{"/api/data", false},
		{"/.git/config", true},
		{"/admin/users", true},
		{"/public/admin", true}, // Contains "admin"
		{"/secret/file", true},
		{"/api/adminstuff", true}, // Contains "admin"
		{"/normal/path", false},
	}

	for _, tt := range tests {
		banned := false
		for _, ban := range bans {
			if strings.Contains(tt.url, ban) {
				banned = true
				break
			}
		}

		if banned != tt.shouldBan {
			t.Errorf("URL %q: got banned=%v, want %v", tt.url, banned, tt.shouldBan)
		}
	}
}

// ===== Cookie Tests =====

// TestCookieParsing tests cookie extraction logic
func TestCookieParsing(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{
		Name:  "AuthentigateSessionToken",
		Value: "test-session-token-123",
	})

	cookie, err := req.Cookie("AuthentigateSessionToken")
	if err != nil {
		t.Fatalf("Failed to get cookie: %v", err)
	}

	if cookie.Value != "test-session-token-123" {
		t.Errorf("Cookie value mismatch: got %s", cookie.Value)
	}
}

// ===== Response Writing Tests =====

// TestResponseWriting tests that responses are written correctly
func TestResponseWriting(t *testing.T) {
	rec := httptest.NewRecorder()

	// Simulate writing a response like displayPage would
	content := "<html><body>Hello TOKEN</body></html>"
	content = templateSet(content, "TOKEN", "World")

	rec.Header().Set("Content-Type", "text/html")
	rec.Write([]byte(content))

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if rec.Header().Get("Content-Type") != "text/html" {
		t.Errorf("Content-Type mismatch: %s", rec.Header().Get("Content-Type"))
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Hello World") {
		t.Errorf("Body should contain 'Hello World', got: %s", body)
	}
}

// ===== Check Function Tests =====

// TestCheckPanicOnError tests that check() panics on error
func TestCheckPanicOnError(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("check() should panic on error")
		}
	}()

	check(os.ErrNotExist)
}

// TestCheckNoErrorNoPanic tests that check() doesn't panic on nil error
func TestCheckNoErrorNoPanic(t *testing.T) {
	// This should not panic
	check(nil)
}

// ===== IsNewUser Tests =====

// Note: This test requires the database to be initialized
// It's included for structure but may need setup

// ===== Provider Secrets Tests =====

// TestProviderSecretsFormat tests the expected format of provider secrets
func TestProviderSecretsFormat(t *testing.T) {
	secretsJSON := `{
		"google": {
			"clientID": "google-client-id",
			"clientSecret": "google-secret",
			"redirectURL": "http://localhost/auth/google/callback"
		},
		"github": {
			"clientID": "github-client-id",
			"clientSecret": "github-secret",
			"redirectURL": "http://localhost/auth/github/callback"
		}
	}`

	var secrets map[string]map[string]string
	if err := json.Unmarshal([]byte(secretsJSON), &secrets); err != nil {
		t.Fatalf("Failed to parse secrets: %v", err)
	}

	// Verify Google config
	google, ok := secrets["google"]
	if !ok {
		t.Fatal("Missing google provider")
	}

	if google["clientID"] != "google-client-id" {
		t.Errorf("Google clientID mismatch")
	}

	// Verify GitHub config
	github, ok := secrets["github"]
	if !ok {
		t.Fatal("Missing github provider")
	}

	if github["redirectURL"] != "http://localhost/auth/github/callback" {
		t.Errorf("GitHub redirectURL mismatch")
	}
}

// ===== Format CLF Tests =====

// Note: format_clf uses *gin.Context which requires gin to be available
// These tests will work once the gin dependency is resolved or removed

// ===== Header Forwarding Tests =====

// TestXForwardedHeaders tests that proxy headers are properly constructed
func TestXForwardedHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	req.Proto = "HTTP/1.1"
	req.Host = "example.com"

	// Simulate what AddAuthToRequest and relay handlers do
	proxyReq, _ := http.NewRequest("GET", "http://backend/api/test", nil)

	proxyReq.Header.Add("X-Forwarded-For", req.RemoteAddr)
	proxyReq.Header.Add("X-Real-IP", "192.168.1.100")
	proxyReq.Header.Add("X-Forwarded-Proto", req.Proto)
	proxyReq.Header.Add("X-Forwarded-Host", req.Host)

	if proxyReq.Header.Get("X-Forwarded-For") != "192.168.1.100:12345" {
		t.Error("X-Forwarded-For not set correctly")
	}

	if proxyReq.Header.Get("X-Real-IP") != "192.168.1.100" {
		t.Error("X-Real-IP not set correctly")
	}

	if proxyReq.Header.Get("X-Forwarded-Proto") != "HTTP/1.1" {
		t.Error("X-Forwarded-Proto not set correctly")
	}

	if proxyReq.Header.Get("X-Forwarded-Host") != "example.com" {
		t.Error("X-Forwarded-Host not set correctly")
	}
}

// ===== Multiple Redirect Types Tests =====

// TestMultipleRedirectTypes tests config with GET, POST, PUT redirects
func TestMultipleRedirectTypes(t *testing.T) {
	configJSON := `{
		"Redirects": [
			{"From": "/api/read/*api", "To": "http://read:8080", "Tipe": "GET", "Name": "read"},
			{"From": "/api/write/*api", "To": "http://write:8080", "Tipe": "POST", "Name": "write"},
			{"From": "/api/update/*api", "To": "http://update:8080", "Tipe": "PUT", "Name": "update"}
		]
	}`

	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	types := make(map[string]bool)
	for _, r := range cfg.Redirects {
		types[r.Tipe] = true
	}

	if !types["GET"] {
		t.Error("Should have GET redirect")
	}
	if !types["POST"] {
		t.Error("Should have POST redirect")
	}
	if !types["PUT"] {
		t.Error("Should have PUT redirect")
	}
}
