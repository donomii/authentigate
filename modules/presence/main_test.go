package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

)

func TestPresenceEscapesStoredUserAndAddresses(t *testing.T) {
	Rooms = nil
	router := http.NewServeMux()
	serveQuester(router, "/presence/")
	host := `<script>alert("host")</script>`
	localIP := `127.0.0.1"><script>alert("local")</script>`
	externalIP := `127.0.0.1"><script>alert("external")</script>`
	query := url.Values{"id": {"7"}, "host": {host}, "localip": {localIP}}
	request := httptest.NewRequest(http.MethodGet, "/presence/users?"+query.Encode(), nil)
	request.Header.Set("X-Tinyproxy", externalIP)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/presence/users?id=7&host=observer", nil))
	if strings.Contains(response.Body.String(), "<script>") || !strings.Contains(response.Body.String(), "&lt;script&gt;") {
		t.Fatalf("stored presence data was not escaped: %s", response.Body.String())
	}
}

func TestRoomPollingKeepsRoomAndEscapesScriptValue(t *testing.T) {
	Rooms = nil
	router := http.NewServeMux()
	serveQuester(router, "/presence/")
	request := httptest.NewRequest(http.MethodGet, "/presence/room?id=7", nil)
	request.Header.Set("authentigate-base-url", `</script><script>alert("token")</script>`)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if !strings.Contains(response.Body.String(), "users?id=7&localip=") {
		t.Fatalf("room polling does not preserve room ID: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), `</script><script>alert("token")</script>`) {
		t.Fatal("base URL escaped the JavaScript string")
	}
}
