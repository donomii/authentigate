package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gopkg.in/resty.v1"
)

func TestFrontendLogsExcludeResponseAndRequestSecrets(t *testing.T) {
	var logs bytes.Buffer
	originalLog := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(originalLog) })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"private-response"}`))
	}))
	defer upstream.Close()
	resp, err := resty.New().R().Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	dumpResponse(resp)
	router := gin.New()
	router.Use(frontendRequestLog())
	router.GET("/purchase", func(c *gin.Context) { c.Status(http.StatusAccepted) })
	router.GET("/failure", func(c *gin.Context) { panic("failed private-session") })
	for _, route := range []string{"/purchase", "/failure"} {
		request := httptest.NewRequest(http.MethodGet, route+"?ccnumber=private-card", nil)
		request.Header.Set("authentigate-token", "private-session")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		expected := http.StatusAccepted
		if route == "/failure" {
			expected = http.StatusInternalServerError
		}
		if response.Code != expected {
			t.Fatalf("%s status=%d, expected=%d", route, response.Code, expected)
		}
	}
	if strings.Contains(logs.String(), "private-") {
		t.Fatalf("frontend logs contain credentials: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "status=403") || !strings.Contains(logs.String(), "status=500") {
		t.Fatal("frontend logs omitted upstream or client status")
	}
}
