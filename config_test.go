package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestConfiguration(t *testing.T, payload []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func expectConfigurationFailure(t *testing.T, path, expected string) {
	t.Helper()
	defer func() {
		message := fmt.Sprint(recover())
		if !strings.Contains(message, path) || !strings.Contains(message, expected) {
			t.Errorf("configuration error: got %q, expected filename %q and %q", message, path, expected)
		}
	}()
	LoadConfig(path)
	t.Error("invalid configuration was accepted")
}

func TestConfigurationDefaults(t *testing.T) {
	path := writeTestConfiguration(t, []byte(`{"BaseUrl":"http://example.test/secure/","Port":8080,"HostNames":["example.test"]}`))
	configuration := LoadConfig(path)
	if !configuration.Secure || configuration.LogFile != "accessLog" {
		t.Errorf("defaults: Secure=%v LogFile=%q", configuration.Secure, configuration.LogFile)
	}
	if configuration.MaxBodyBytes != defaultMaxBodyBytes || configuration.UpstreamTimeoutSeconds != defaultUpstreamTimeoutSeconds {
		t.Errorf("body/timeout defaults: %d bytes, %d seconds", configuration.MaxBodyBytes, configuration.UpstreamTimeoutSeconds)
	}
}

func TestConfigurationRejectsMalformedJSON(t *testing.T) {
	for _, payload := range []string{`{`, `{"Port":"eighty"}`, `{"Redirects":[{"CopyHeaders":"Authorization"}]}`} {
		path := writeTestConfiguration(t, []byte(payload))
		expectConfigurationFailure(t, path, "Unable to decode config file")
	}
}

func TestConfigurationRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name     string
		change   func(*Config)
		expected string
	}{
		{"relative base URL", func(config *Config) { config.BaseUrl = "/secure/" }, "BaseUrl"},
		{"base URL missing slash", func(config *Config) { config.BaseUrl = "https://example.test/secure" }, "BaseUrl"},
		{"base URL missing hostname", func(config *Config) { config.BaseUrl = "http://:8080/" }, "BaseUrl"},
		{"port zero", func(config *Config) { config.Port = 0 }, "Port"},
		{"port too large", func(config *Config) { config.Port = 65536 }, "Port"},
		{"TLS without certificate hostname", func(config *Config) { config.Secure = true }, "HostNames"},
		{"empty log filename", func(config *Config) { config.LogFile = "" }, "LogFile"},
		{"zero body limit", func(config *Config) { config.MaxBodyBytes = 0 }, "MaxBodyBytes"},
		{"negative body limit", func(config *Config) { config.MaxBodyBytes = -1 }, "MaxBodyBytes"},
		{"overflowing body limit", func(config *Config) { config.MaxBodyBytes = 1<<63 - 1 }, "MaxBodyBytes"},
		{"zero timeout", func(config *Config) { config.UpstreamTimeoutSeconds = 0 }, "UpstreamTimeoutSeconds"},
		{"overflowing timeout", func(config *Config) { config.UpstreamTimeoutSeconds = 1 << 62 }, "UpstreamTimeoutSeconds"},
		{"relative target", func(config *Config) { config.Redirects[0].To = "/relative" }, "To"},
		{"target missing hostname", func(config *Config) { config.Redirects[0].To = "http://:8080/" }, "To"},
		{"target unsupported scheme", func(config *Config) { config.Redirects[0].To = "ftp://example.test/" }, "To"},
		{"target fragment", func(config *Config) { config.Redirects[0].To = "http://example.test/#fragment" }, "To"},
		{"missing route name", func(config *Config) { config.Redirects[0].Name = "" }, "Name"},
		{"relative route path", func(config *Config) { config.Redirects[0].From = "relative" }, "From"},
		{"unsupported method", func(config *Config) { config.Redirects[0].Tipe = "DELETE" }, "Tipe"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			configuration := Config{
				BaseUrl: "http://example.test/secure/", Port: 8080, LogFile: "accessLog", Secure: false,
				MaxBodyBytes: 1024, UpstreamTimeoutSeconds: 1,
				Redirects: []Redirect{{Name: "demo", From: "/secure/:token/demo/*api", To: "http://127.0.0.1:8081", Tipe: "GET"}},
			}
			test.change(&configuration)
			payload, err := json.Marshal(configuration)
			if err != nil {
				t.Fatal(err)
			}
			path := writeTestConfiguration(t, payload)
			expectConfigurationFailure(t, path, test.expected)
		})
	}
}

func TestBundledConfigurationLoads(t *testing.T) {
	for _, path := range []string{"config.json", "example_config.json"} {
		t.Run(path, func(t *testing.T) {
			if configuration := LoadConfig(path); len(configuration.Redirects) == 0 {
				t.Error("bundled configuration has no routes")
			}
		})
	}
}
