package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMenuLogsExcludeRenderedTokens(t *testing.T) {
	var logs bytes.Buffer
	originalLog := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(originalLog) })
	page := `<a href="/secure/private-session/">Continue</a>`
	filename := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(filename, []byte(page), 0600); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/menu", nil)
	request.Header.Set("authentigate-id", "menu-user")
	request.Header.Set("authentigate-base-url", "/secure/private-session/menu/")
	response := httptest.NewRecorder()
	makeAuthed(func(c *Context, id, base, top string) { displayStaticPage(c, filename) })(response, request)
	if response.Body.String() != page {
		t.Fatal("menu page changed")
	}
	if strings.Contains(logs.String(), "private-session") || !strings.Contains(logs.String(), "menu-user") {
		t.Fatalf("unsafe or incomplete menu logs: %s", logs.String())
	}
}
