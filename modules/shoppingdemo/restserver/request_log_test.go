package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestShopLogsExcludeHeadersAndQueryCredentials(t *testing.T) {
	var logs bytes.Buffer
	originalLog := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(originalLog) })
	router := gin.New()
	router.Use(shopRequestLog())
	router.GET("/shop", makeAuthed(func(c *gin.Context, id, base string) { c.Status(http.StatusAccepted) }))
	router.GET("/failure", makeAuthed(func(c *gin.Context, id, base string) {
		panic(fmt.Sprintf("backend rejected token %s", c.Request.Header.Get("authentigate-token")))
	}))
	for _, route := range []string{"/shop", "/failure"} {
		request := httptest.NewRequest(http.MethodGet, route+"?ccnumber=private-card&code=private-code", nil)
		request.Header.Set("authentigate-id", "shop-user")
		request.Header.Set("authentigate-token", "private-session")
		request.Header.Set("authentigate-top-url", "/secure/private-session/")
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
		t.Fatalf("shop logs contain credentials: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "backend rejected") || !strings.Contains(logs.String(), "status=500") {
		t.Fatal("shop logs omitted the cause or status")
	}
}
