package main

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestProxyLoggingDoesNotExposeCredentials(t *testing.T) {
	var logs, access bytes.Buffer
	originalLog, originalStdout := log.Writer(), os.Stdout
	originalConfig, originalAccess := config, accessLog
	stdout, err := os.CreateTemp(t.TempDir(), "stdout-")
	if err != nil {
		t.Fatal(err)
	}
	log.SetOutput(&logs)
	os.Stdout = stdout
	accessLog = bufio.NewWriter(&access)
	t.Cleanup(func() {
		log.SetOutput(originalLog)
		os.Stdout = originalStdout
		config, accessLog = originalConfig, originalAccess
		stdout.Close()
	})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Write([]byte("backend response"))
	}))
	defer upstream.Close()
	config = &Config{Redirects: []Redirect{{Host: "gateway.example", To: upstream.URL}}, Port: 9000}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		request := httptest.NewRequest(method, "http://gateway.example/secure/private-session/api?code=private-code", strings.NewReader("password=private-body"))
		request.Header.Set("Authentigate-Token", "private-session")
		request.Header.Set("Authorization", "Bearer private-authorization")
		request.Header.Set("Referer", "http://gateway.example/secure/private-referrer")
		response := httptest.NewRecorder()
		MyMux{}.ServeHTTP(response, request)
		if response.Body.String() != "backend response" {
			t.Fatalf("%s response=%q", method, response.Body.String())
		}
	}
	rootRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	logProxyFailure(&MyContext{Request: *rootRequest}, `Get "http://user:private-password@backend/": connection refused`)
	if err := accessLog.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := stdout.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatal(err)
	}
	combined := logs.String() + access.String() + string(output)
	if strings.Contains(combined, "private-") {
		t.Fatalf("proxy logs expose request secrets: %s", combined)
	}
	if !strings.Contains(combined, "gateway.example") || !strings.Contains(access.String(), "200") {
		t.Fatal("proxy logs omitted host or upstream status")
	}
}
