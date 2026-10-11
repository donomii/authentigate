package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func startProxy(t *testing.T, redirects []Redirect) (*httptest.Server, string) {
	t.Helper()
	previousConfig, previousLog := config, accessLog
	file, err := os.CreateTemp(t.TempDir(), "access-log-")
	if err != nil {
		t.Fatal(err)
	}
	config = &Config{Redirects: redirects}
	accessLog = log.New(file, "", 0)
	server := httptest.NewServer(MyMux{})
	t.Cleanup(func() {
		server.Close()
		config, accessLog = previousConfig, previousLog
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	})
	return server, file.Name()
}

func proxyRequest(t *testing.T, server *httptest.Server, method, path, host string, body io.Reader) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, server.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = host
	client := server.Client()
	client.Timeout = 5 * time.Second
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

func readResponse(t *testing.T, response *http.Response) string {
	t.Helper()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func TestHostRouting(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fmt.Fprint(writer, request.URL.Path)
	}))
	t.Cleanup(upstream.Close)
	proxy, _ := startProxy(t, []Redirect{
		{Host: "example.com", To: upstream.URL + "/parent"},
		{Host: "app.example.com", To: upstream.URL + "/app"},
	})
	cases := []struct {
		host   string
		status int
		body   string
	}{
		{"example.com", 200, "/parent/"},
		{"EXAMPLE.COM:3000", 200, "/parent/"},
		{"example.com.", 200, "/parent/"},
		{"sub.example.com", 200, "/parent/"},
		{"app.example.com", 200, "/app/"},
		{"child.app.example.com", 200, "/app/"},
		{"notexample.com", 404, "404 - No route configured for this host\n"},
		{"example.com.attacker.invalid", 404, "404 - No route configured for this host\n"},
	}
	for _, test := range cases {
		t.Run(test.host, func(t *testing.T) {
			response := proxyRequest(t, proxy, "GET", "/", test.host, nil)
			body := readResponse(t, response)
			if response.StatusCode != test.status || body != test.body {
				t.Fatalf("host %q: received %d %q, expected %d %q", test.host, response.StatusCode, body, test.status, test.body)
			}
		})
	}
}

func TestRelayMethodsPreserveRequestAndResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		writer.Header().Set("Received-Method", request.Method)
		writer.Header().Set("Received-URI", request.RequestURI)
		writer.Header().Set("Received-Body", string(body))
		writer.Header().Add("Set-Cookie", "first=1; Path=/")
		writer.Header().Add("Set-Cookie", "second=2; Path=/")
		writer.Header().Set("Connection", "Upstream-Only")
		writer.Header().Set("Upstream-Only", "remove")
		writer.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(writer, "upstream failure")
	}))
	t.Cleanup(upstream.Close)
	proxy, _ := startProxy(t, []Redirect{{Host: "example.com", To: upstream.URL + "/api?fixed=1"}})
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"} {
		t.Run(method, func(t *testing.T) {
			response := proxyRequest(t, proxy, method, "/part%2Fname?item=a%2Fb&item=c", "example.com", strings.NewReader("payload"))
			body := readResponse(t, response)
			if response.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status: got %d, expected %d", response.StatusCode, http.StatusUnprocessableEntity)
			}
			if received := response.Header.Get("Received-URI"); received != "/api/part%2Fname?fixed=1&item=a%2Fb&item=c" {
				t.Errorf("upstream URI: %q", received)
			}
			if response.Header.Get("Received-Method") != method || response.Header.Get("Received-Body") != "payload" {
				t.Errorf("upstream method/body: %q %q", response.Header.Get("Received-Method"), response.Header.Get("Received-Body"))
			}
			if cookies := response.Header.Values("Set-Cookie"); len(cookies) != 2 {
				t.Errorf("received cookies %v, expected both upstream cookies", cookies)
			}
			if response.Header.Get("Upstream-Only") != "" {
				t.Error("upstream connection-specific header reached client")
			}
			if method == "HEAD" {
				if body != "" {
					t.Errorf("HEAD returned a body: %q", body)
				}
			} else if body != "upstream failure" {
				t.Errorf("body: %q", body)
			}
		})
	}
}

func TestRelayDoesNotFollowRedirects(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/destination" {
			t.Error("proxy followed upstream redirect")
		}
		writer.Header().Set("Location", "/destination")
		writer.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(upstream.Close)
	proxy, _ := startProxy(t, []Redirect{{Host: "example.com", To: upstream.URL}})
	for _, method := range []string{"GET", "POST", "PUT"} {
		response := proxyRequest(t, proxy, method, "/start", "example.com", strings.NewReader("payload"))
		readResponse(t, response)
		if response.StatusCode != http.StatusTemporaryRedirect || response.Header.Get("Location") != "/destination" {
			t.Errorf("%s: status %d, location %q", method, response.StatusCode, response.Header.Get("Location"))
		}
	}
}

func TestRelayRebuildsForwardingHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		for _, name := range []string{"X-Forwarded-For", "X-Real-IP", "X-Forwarded-Proto", "X-Forwarded-Host", "X-Forwarded-Port", "Forwarded", "Client-Only"} {
			writer.Header().Set("Received-"+name, request.Header.Get(name))
		}
	}))
	t.Cleanup(upstream.Close)
	proxy, _ := startProxy(t, []Redirect{{Host: "example.com", To: upstream.URL}})
	request, err := http.NewRequest("GET", proxy.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "example.com:3000"
	for _, name := range []string{"X-Forwarded-For", "X-Real-IP", "X-Forwarded-Proto", "X-Forwarded-Host", "X-Forwarded-Port", "Forwarded", "Client-Only"} {
		request.Header.Set(name, "spoofed")
	}
	request.Header.Set("Connection", "Client-Only, X-Real-IP")
	response, err := proxy.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	readResponse(t, response)
	for name, expected := range map[string]string{
		"X-Forwarded-For": "127.0.0.1", "X-Real-IP": "127.0.0.1", "X-Forwarded-Proto": "http",
		"X-Forwarded-Host": "example.com:3000", "X-Forwarded-Port": "3000", "Forwarded": "", "Client-Only": "",
	} {
		if received := response.Header.Get("Received-" + name); received != expected {
			t.Errorf("%s: got %q, expected %q", name, received, expected)
		}
	}
}

func TestRelayUnavailableUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	upstream.Close()
	proxy, _ := startProxy(t, []Redirect{{Host: "example.com", To: upstream.URL}})
	for _, method := range []string{"GET", "POST", "PUT"} {
		response := proxyRequest(t, proxy, method, "/", "example.com", nil)
		readResponse(t, response)
		if response.StatusCode != http.StatusBadGateway {
			t.Errorf("%s: status %d, expected 502", method, response.StatusCode)
		}
	}
}

func TestRelayStreamsResponse(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(writer, "first")
		if err := http.NewResponseController(writer).Flush(); err != nil {
			t.Error(err)
			return
		}
		<-release
		fmt.Fprint(writer, "second")
	}))
	t.Cleanup(upstream.Close)
	proxy, _ := startProxy(t, []Redirect{{Host: "example.com", To: upstream.URL}})
	t.Cleanup(func() { close(release) })
	response := proxyRequest(t, proxy, "GET", "/", "example.com", nil)
	payload := make([]byte, 5)
	if _, err := io.ReadFull(response.Body, payload); err != nil {
		t.Fatal(err)
	}
	if string(payload) != "first" {
		t.Fatalf("first response chunk: %q", payload)
	}
}

func TestConcurrentAccessLogging(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fmt.Fprint(writer, "response")
	}))
	t.Cleanup(upstream.Close)
	proxy, logPath := startProxy(t, []Redirect{{Host: "example.com", To: upstream.URL}})
	const requests = 40
	var workers sync.WaitGroup
	for index := 0; index < requests; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			request, err := http.NewRequest("GET", proxy.URL, nil)
			if err != nil {
				t.Error(err)
				return
			}
			request.Host = "example.com"
			response, err := proxy.Client().Do(request)
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := io.Copy(io.Discard, response.Body); err != nil {
				t.Error(err)
			}
			if err := response.Body.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	proxy.Close()
	payload, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(payload), "\n"); count != requests {
		t.Errorf("received %d access log lines, expected %d", count, requests)
	}
}

func TestRelayDoesNotLogCredentialsOrBodies(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "server-log-")
	if err != nil {
		t.Fatal(err)
	}
	previousOutput := log.Writer()
	log.SetOutput(file)
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
		file.Close()
	})
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		io.Copy(io.Discard, request.Body)
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)
	proxy, accessPath := startProxy(t, []Redirect{{Host: "example.com", To: upstream.URL}})
	request, err := http.NewRequest("POST", proxy.URL, strings.NewReader("private-body-value"))
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "example.com"
	request.Header.Set("Authorization", "Bearer private-token-value")
	request.Header.Set("Cookie", "session=private-cookie-value")
	response, err := proxy.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	readResponse(t, response)
	proxy.Close()
	for _, path := range []string{file.Name(), accessPath} {
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"private-body-value", "private-token-value", "private-cookie-value"} {
			if strings.Contains(string(payload), secret) {
				t.Errorf("%s contains credential/body %q", path, secret)
			}
		}
	}
}
