package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func shopFiles(t *testing.T, files map[string]string) *gin.Engine {
	t.Helper()
	directory := t.TempDir()
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(http.FileServer(http.Dir(directory)))
	t.Cleanup(server.Close)
	previous := shopService
	shopService = server.URL + "/"
	t.Cleanup(func() { shopService = previous })
	router := gin.New()
	serveDemo(router, "/")
	return router
}

func TestPurchaseDoesNotConfirmFailedServiceRequest(t *testing.T) {
	router := shopFiles(t, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/purchase?coupon=none", nil))
	if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "Purchase confirmed") {
		t.Fatalf("failed service request reported success: status %d, body %s", response.Code, response.Body.String())
	}
}

func TestInvalidServiceJSONReturnsGatewayError(t *testing.T) {
	router := shopFiles(t, map[string]string{"purchase": `{"orange":`})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/purchase", nil))
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "invalid JSON") {
		t.Fatalf("invalid JSON response: status %d, body %s", response.Code, response.Body.String())
	}
}

func TestBasketEscapesItemHTML(t *testing.T) {
	router := shopFiles(t, map[string]string{"basket": `{"<script>alert(1)</script>":1}`})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/basket", nil))
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "<script>") || !strings.Contains(response.Body.String(), "&lt;script&gt;") {
		t.Fatalf("basket item not escaped: status %d, body %s", response.Code, response.Body.String())
	}
}
