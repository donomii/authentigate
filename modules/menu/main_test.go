package main

import (
	"bytes"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func menuDatabase(t *testing.T) {
	t.Helper()
	var err error
	b, err = newServer(filepath.Join(t.TempDir(), "messages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.db.Close() })
}

func TestMessageCheckStartsEmptyAndConsumesShortMessages(t *testing.T) {
	menuDatabase(t)
	router := gin.New()
	router.GET("/check", makeAuthed(checkHandler))
	check := func(expected string) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/check", nil)
		request.Header.Set("authentigate-id", "reader")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.String() != expected {
			t.Fatalf("message check: status %d, body %q; expected %q", response.Code, response.Body.String(), expected)
		}
	}
	check("none")
	if err := b.Put("message", "reader", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if message := fetchMessage("reader"); message != "x" {
		t.Fatalf("one-byte message lost: %q", message)
	}
	check("x")
	check("none")
}

func TestUploadValidatesMultipartAndStoresImage(t *testing.T) {
	menuDatabase(t)
	router := gin.New()
	router.POST("/upload", makeAuthed(uploadHandler))
	imageData := &bytes.Buffer{}
	if err := png.Encode(imageData, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	for _, upload := range []struct {
		name   string
		data   []byte
		status int
	}{
		{"missing image", nil, http.StatusBadRequest},
		{"PNG image", imageData.Bytes(), http.StatusOK},
		{"HTML document", []byte("<html><script>alert(1)</script></html>"), http.StatusUnsupportedMediaType},
		{"oversized image", bytes.Repeat([]byte("x"), 33<<20), http.StatusRequestEntityTooLarge},
	} {
		body := &bytes.Buffer{}
		form := multipart.NewWriter(body)
		if upload.data != nil {
			file, err := form.CreateFormFile("upimage", "camera.png")
			if err != nil {
				t.Fatal(err)
			}
			file.Write(upload.data)
		}
		form.Close()
		request := httptest.NewRequest(http.MethodPost, "/upload", body)
		request.Header.Set("Content-Type", form.FormDataContentType())
		request.Header.Set("authentigate-id", "camera")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != upload.status {
			t.Fatalf("%s: status %d, body %s; expected status %d", upload.name, response.Code, response.Body.String(), upload.status)
		}
		if upload.status == http.StatusOK && fetchMessage("camera") != imageData.String() {
			t.Fatal("valid upload did not store the PNG bytes")
		}
	}
}

func TestStoredMessagesCannotExecuteAsHTML(t *testing.T) {
	menuDatabase(t)
	router := gin.New()
	router.GET("/image", makeAuthed(imageHandler))
	router.GET("/check", makeAuthed(checkHandler))
	if err := storeMessage("reader", "<script>alert(1)</script>"); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/image", "/check"} {
		request := httptest.NewRequest(http.MethodGet, endpoint, nil)
		request.Header.Set("authentigate-id", "reader")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if endpoint == "/image" {
			if response.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("HTML served as an image: status %d", response.Code)
			}
		} else if response.Header().Get("Content-Type") != "text/plain; charset=utf-8" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("message response permits HTML interpretation: %v", response.Header())
		}
	}
}

func TestTokenDisplayEscapesHTML(t *testing.T) {
	router := gin.New()
	router.GET("/token", func(c *gin.Context) { displayPage(c, c.Query("id"), "files/metro/1.html") })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/token?id=%3Cscript%3Ealert(1)%3C%2Fscript%3E", nil))
	if strings.Contains(response.Body.String(), "<script>alert(1)</script>") || !strings.Contains(response.Body.String(), "&lt;script&gt;") {
		t.Fatalf("token is not escaped in rendered page: %s", response.Body.String())
	}
}

func TestTokenActionsUseGatewayManagement(t *testing.T) {
	router := gin.New()
	router.GET("/token", makeAuthed(tokenShowHandler))
	router.GET("/api/newToken", makeAuthed(newTokenHandler))
	for endpoint, target := range map[string]string{"/token": "/manage/current-token/token", "/api/newToken": "/manage/current-token/newToken"} {
		request := httptest.NewRequest(http.MethodGet, endpoint, nil)
		request.Header.Set("authentigate-token", "current-token")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != target {
			t.Fatalf("%s: status %d, target %q; expected %q", endpoint, response.Code, response.Header().Get("Location"), target)
		}
	}
}

func TestRemoteShutdownIsDisabled(t *testing.T) {
	router := gin.New()
	router.GET("/api/shutdown", makeAuthed(shutdownHandler))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/shutdown", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("remote shutdown returned HTTP %d; expected HTTP 403", response.Code)
	}
}
