package main

import (
	"os"
	"testing"
)

// ===== User Save/Load Tests =====

// Note: These tests require the authDB to be properly initialized
// They test the public interface (SaveUser, LoadUser, etc.)

// TestSaveAndLoadUserIntegration tests user persistence
func TestSaveAndLoadUserIntegration(t *testing.T) {
	t.Skip("Requires database initialization - enable after go mod tidy")

	user := &userData_t{
		Id:         "user-test-123",
		ForeignIDs: []string{"google-abc", "github-def"},
		Token:      "session-token-xyz",
		OtpSecret:  "JBSWY3DPEHPK3PXP",
		UserName:   "testuser",
	}

	SaveUser(user)

	loaded := LoadUser("user-test-123")
	if loaded == nil {
		t.Fatal("LoadUser returned nil")
	}

	if loaded.Id != user.Id {
		t.Errorf("Id mismatch: got %s, want %s", loaded.Id, user.Id)
	}

	if loaded.Token != user.Token {
		t.Errorf("Token mismatch: got %s, want %s", loaded.Token, user.Token)
	}

	if loaded.UserName != user.UserName {
		t.Errorf("UserName mismatch: got %s, want %s", loaded.UserName, user.UserName)
	}

	if len(loaded.ForeignIDs) != 2 {
		t.Errorf("ForeignIDs length mismatch: got %d", len(loaded.ForeignIDs))
	}
}

// TestLoadUserNonexistent tests LoadUser for nonexistent user
func TestLoadUserNonexistent(t *testing.T) {
	t.Skip("Requires database initialization - enable after go mod tidy")

	user := LoadUser("nonexistent-user-id-xyz")
	if user != nil {
		t.Error("LoadUser should return nil for nonexistent user")
	}
}

// ===== Session Token Tests =====

// TestSessionTokenToIdIntegration tests session token lookup
func TestSessionTokenToIdIntegration(t *testing.T) {
	t.Skip("Requires database initialization - enable after go mod tidy")

	// This test requires b.SessionTokens to be initialized
	id := sessionTokenToId("test-session-token")
	// Just verify it doesn't panic
	_ = id
}

// TestSessionTokenToIdNonexistent tests session token lookup for nonexistent token
func TestSessionTokenToIdNonexistent(t *testing.T) {
	t.Skip("Requires database initialization - enable after go mod tidy")

	id := sessionTokenToId("nonexistent-token")
	if id != "" {
		t.Errorf("sessionTokenToId should return empty for nonexistent token, got %s", id)
	}
}

// ===== Foreign ID Tests =====

// TestForeignIdToIdIntegration tests foreign ID lookup
func TestForeignIdToIdIntegration(t *testing.T) {
	t.Skip("Requires database initialization - enable after go mod tidy")

	// This test requires b.ForeignIDs to be initialized
	id := foreignIdToId("google-user-123")
	// Just verify it doesn't panic
	_ = id
}

// TestForeignIdToIdNonexistent tests foreign ID lookup for nonexistent ID
func TestForeignIdToIdNonexistent(t *testing.T) {
	t.Skip("Requires database initialization - enable after go mod tidy")

	id := foreignIdToId("nonexistent-foreign-id")
	if id != "" {
		t.Errorf("foreignIdToId should return empty for nonexistent ID, got %s", id)
	}
}

// ===== Config Load Tests =====

// TestLoadConfigFile tests loading config from a file
func TestLoadConfigFile(t *testing.T) {
	configJSON := `{
		"Redirects": [
			{"From": "/api/*api", "To": "http://backend:8080", "Tipe": "GET", "Name": "api"}
		],
		"Port": 9000,
		"BaseUrl": "http://test.local/secure/",
		"HostNames": ["test.local"],
		"Bans": [".git"],
		"LogFile": "test.log",
		"Secure": false
	}`

	tmpFile := t.TempDir() + "/config.json"
	if err := os.WriteFile(tmpFile, []byte(configJSON), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg := LoadConfig(tmpFile)
	if cfg == nil {
		t.Fatal("LoadConfig returned nil")
	}

	if cfg.Port != 9000 {
		t.Errorf("Port mismatch: got %d", cfg.Port)
	}

	if cfg.BaseUrl != "http://test.local/secure/" {
		t.Errorf("BaseUrl mismatch: got %s", cfg.BaseUrl)
	}
}

// TestLoadConfigMissingFile tests LoadConfig with missing file - should panic
func TestLoadConfigMissingFile(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("LoadConfig should panic for missing file")
		}
	}()

	LoadConfig("/nonexistent/path/to/config.json")
}

// TestLoadConfigInvalidJSON tests LoadConfig with invalid JSON
func TestLoadConfigInvalidJSON(t *testing.T) {
	tmpFile := t.TempDir() + "/invalid_config.json"
	if err := os.WriteFile(tmpFile, []byte("{ not valid json"), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// This might panic or return a default config depending on implementation
	// Just verify it doesn't crash unexpectedly
	cfg := LoadConfig(tmpFile)
	// If it doesn't panic, we should have some kind of config
	_ = cfg
}
