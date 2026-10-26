package main

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

)

const relayTestUserID = "4bfebcb1-35a7-4bb5-b98a-7c6a84cc4661"
const relayTestToken = "6085262d-f90a-4ee6-883b-ff1cb497b801"
const relayTestPath = "/secure/" + relayTestToken + "/demo"

func startAuthenticatedProxy(t *testing.T, target string, settings Config, copyHeaders []string) *httptest.Server {
	t.Helper()
	previousDB, previousPrefix := b, db_prefix
	previousConfig, previousBaseURL, previousLog := config, baseUrl, accessLog

	db_prefix = filepath.Join(t.TempDir(), "auth-")
	database, err, shutdown := newAuthDB(db_prefix)
	if err != nil {
		t.Fatal(err)
	}
	b = database
	t.Cleanup(func() {
		shutdown()
		b, db_prefix = previousDB, previousPrefix
		config, baseUrl, accessLog = previousConfig, previousBaseURL, previousLog

	})
	user := userData_t{Id: relayTestUserID, Token: relayTestToken}
	if err := b.Users.Set(user.Id, user); err != nil {
		t.Fatal(err)
	}
	if err := b.SessionTokens.Set(user.Token, user.Id); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.CreateTemp(t.TempDir(), "access-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := logFile.Close(); err != nil {
			t.Error(err)
		}
	})
	accessLog = logFile
	config = &settings
	baseUrl = "http://edge.example:8080/secure/"
	router := http.NewServeMux()
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		relay := Redirect{From: "/secure/{token}/demo/{api...}", To: target, Tipe: method, Name: "demo", CopyHeaders: copyHeaders}
		switch method {
		case http.MethodGet:
			router.HandleFunc("GET "+relay.From, makeAuthedRelay(relayGetHandler, &relay, nil))
		case http.MethodPost:
			router.HandleFunc("POST "+relay.From, makeAuthedRelay(relayPostHandler, &relay, nil))
		case http.MethodPut:
			router.HandleFunc("PUT "+relay.From, makeAuthedRelay(relayPutHandler, &relay, nil))
		}
	}
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

func requestThroughProxy(t *testing.T, server *httptest.Server, method, path string, body io.Reader, headers http.Header) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, server.URL+relayTestPath+path, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header = headers.Clone()
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

func readProxyResponse(t *testing.T, response *http.Response) string {
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

func TestProxyPreservesRequestAndResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		payload, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		writer.Header().Set("Received-Method", request.Method)
		writer.Header().Set("Received-URI", request.RequestURI)
		writer.Header().Set("Received-Body", string(payload))
		writer.Header().Add("Set-Cookie", "first=1; Path=/")
		writer.Header().Add("Set-Cookie", "second=2; Path=/")
		writer.Header().Set("Connection", "Upstream-Only")
		writer.Header().Set("Upstream-Only", "remove")
		writer.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(writer, "rejected by upstream")
	}))
	t.Cleanup(upstream.Close)
	proxy := startAuthenticatedProxy(t, upstream.URL+"/api?fixed=1", Config{}, nil)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			response := requestThroughProxy(t, proxy, method, "/part%2Fname/%E2%98%83?item=a%2Fb&item=c", strings.NewReader("request-payload"), nil)
			body := readProxyResponse(t, response)
			if response.StatusCode != http.StatusUnprocessableEntity || body != "rejected by upstream" {
				t.Errorf("response: got %d %q, expected 422 with upstream body", response.StatusCode, body)
			}
			if received := response.Header.Get("Received-URI"); received != "/api/part%2Fname/%E2%98%83?fixed=1&item=a%2Fb&item=c" {
				t.Errorf("upstream URI: %q", received)
			}
			if response.Header.Get("Received-Method") != method || response.Header.Get("Received-Body") != "request-payload" {
				t.Errorf("upstream method/body: %q %q", response.Header.Get("Received-Method"), response.Header.Get("Received-Body"))
			}
			if cookies := response.Header.Values("Set-Cookie"); len(cookies) != 2 {
				t.Errorf("upstream cookies: got %v, expected both cookies", cookies)
			}
			if response.Header.Get("Upstream-Only") != "" {
				t.Error("upstream connection-specific header reached client")
			}
		})
	}
}

func TestProxyHeaderAllowlistAndTrustedIdentity(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		for name, values := range request.Header {
			for _, value := range values {
				writer.Header().Add("Received-"+name, value)
			}
		}
	}))
	t.Cleanup(upstream.Close)
	allowed := []string{"Authorization", "X-Pass", "X-Hop", "authentigate-id", "AUTHENTIGATE-TOKEN", "X-Forwarded-For", "Forwarded"}
	proxy := startAuthenticatedProxy(t, upstream.URL, Config{}, allowed)
	headers := http.Header{
		"Authorization": {"Bearer allowed"}, "X-Pass": {"first", "second"}, "X-Drop": {"remove"},
		"Connection": {"X-Hop"}, "X-Hop": {"remove"}, "Authentigate-Id": {"attacker"},
		"Authentigate-Token": {"forged"}, "X-Forwarded-For": {"203.0.113.7"}, "Forwarded": {"for=203.0.113.7"},
		"Content-Type": {"application/json"}, "Cookie": {"private=must-not-leak"},
	}
	response := requestThroughProxy(t, proxy, http.MethodPost, "/headers", strings.NewReader("{}"), headers)
	readProxyResponse(t, response)
	for name, expected := range map[string]string{
		"Authorization": "Bearer allowed", "X-Drop": "", "X-Hop": "", "Cookie": "", "Forwarded": "",
		"Authentigate-Id": relayTestUserID, "Authentigate-Token": relayTestToken,
		"Authentigate-Base-Url": baseUrl + "c/demo/", "Authentigate-Base-Token-Url": baseUrl + relayTestToken + "/demo/",
		"Authentigate-Top-Url": baseUrl + relayTestToken + "/", "X-Forwarded-For": "127.0.0.1",
		"X-Real-IP": "127.0.0.1", "X-Forwarded-Proto": "http", "Content-Type": "application/json",
	} {
		if received := response.Header.Get("Received-" + name); received != expected {
			t.Errorf("%s: got %q, expected %q", name, received, expected)
		}
	}
	if values := response.Header.Values("Received-X-Pass"); len(values) != 2 || values[0] != "first" || values[1] != "second" {
		t.Errorf("allowed multi-valued header: %v", values)
	}
}

func TestProxyPreservesEncodedTargetPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fmt.Fprint(writer, request.RequestURI)
	}))
	t.Cleanup(upstream.Close)
	proxy := startAuthenticatedProxy(t, upstream.URL+"/api%2F", Config{}, nil)
	response := requestThroughProxy(t, proxy, http.MethodGet, "/item", nil, nil)
	if received := readProxyResponse(t, response); received != "/api%2F/item" {
		t.Errorf("upstream URI: got %q, expected /api%%2F/item", received)
	}
}

func TestProxyDoesNotFollowRedirects(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destinationCalls.Add(1) }))
	t.Cleanup(destination.Close)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Location", destination.URL+"/capture")
		writer.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(upstream.Close)
	proxy := startAuthenticatedProxy(t, upstream.URL, Config{}, nil)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		response := requestThroughProxy(t, proxy, method, "/redirect", strings.NewReader("private-body"), nil)
		readProxyResponse(t, response)
		if response.StatusCode != http.StatusTemporaryRedirect || response.Header.Get("Location") != destination.URL+"/capture" {
			t.Errorf("%s redirect: status=%d location=%q", method, response.StatusCode, response.Header.Get("Location"))
		}
	}
	if count := destinationCalls.Load(); count != 0 {
		t.Errorf("redirect destination received %d requests containing proxy credentials", count)
	}
}

func TestProxyConnectionFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	upstream.Close()
	proxy := startAuthenticatedProxy(t, upstream.URL, Config{}, nil)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		response := requestThroughProxy(t, proxy, method, "/offline", nil, nil)
		body := readProxyResponse(t, response)
		if response.StatusCode != http.StatusBadGateway || !strings.Contains(body, "demo") {
			t.Errorf("%s connection failure: status=%d body=%q", method, response.StatusCode, body)
		}
	}
}

func TestProxyBodyLimits(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.URL.Path == "/gzip" {
			writer.Header().Set("Content-Encoding", "gzip")
			compressed := gzip.NewWriter(writer)
			if _, err := compressed.Write([]byte(strings.Repeat("x", 33))); err != nil {
				t.Error(err)
			}
			if err := compressed.Close(); err != nil {
				t.Error(err)
			}
		} else {
			fmt.Fprint(writer, strings.Repeat("x", 33))
		}
	}))
	t.Cleanup(upstream.Close)
	proxy := startAuthenticatedProxy(t, upstream.URL, Config{MaxBodyBytes: 32}, nil)
	response := requestThroughProxy(t, proxy, http.MethodPost, "/large-request", strings.NewReader(strings.Repeat("x", 33)), nil)
	readProxyResponse(t, response)
	if response.StatusCode != http.StatusRequestEntityTooLarge || calls.Load() != 0 {
		t.Errorf("oversized request: status=%d upstream calls=%d", response.StatusCode, calls.Load())
	}
	for _, path := range []string{"/large-response", "/gzip"} {
		response := requestThroughProxy(t, proxy, http.MethodGet, path, nil, nil)
		body := readProxyResponse(t, response)
		if response.StatusCode != http.StatusBadGateway || !strings.Contains(body, "32 bytes") {
			t.Errorf("oversized response %s: status=%d body=%q", path, response.StatusCode, body)
		}
	}
}

func TestProxyTimeouts(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/body" {
			writer.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(writer, "partial")
			if err := http.NewResponseController(writer).Flush(); err != nil {
				t.Error(err)
				return
			}
		}
		<-request.Context().Done()
	}))
	t.Cleanup(upstream.Close)
	proxy := startAuthenticatedProxy(t, upstream.URL, Config{UpstreamTimeoutSeconds: 1}, nil)
	for _, path := range []string{"/headers", "/body"} {
		response := requestThroughProxy(t, proxy, http.MethodGet, path, nil, nil)
		body := readProxyResponse(t, response)
		if response.StatusCode != http.StatusGatewayTimeout {
			t.Errorf("timeout %s: status=%d body=%q, expected 504", path, response.StatusCode, body)
		}
	}
}

func TestProxyPropagatesClientCancellation(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
		close(cancelled)
	}))
	t.Cleanup(upstream.Close)
	proxy := startAuthenticatedProxy(t, upstream.URL, Config{UpstreamTimeoutSeconds: 2}, nil)
	requestContext, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, proxy.URL+relayTestPath+"/cancel", nil)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		response, err := proxy.Client().Do(request)
		if response != nil {
			response.Body.Close()
		}
		result <- err
	}()
	select {
	case <-started:
		cancel()
	case <-time.After(3 * time.Second):
		t.Fatal("upstream request never started")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("upstream context remained active after client cancellation")
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Errorf("client result: got %v, expected cancellation", err)
	}
}

func TestProxyCameraStreamOutlivesBufferedTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
		fmt.Fprint(writer, "first-frame")
		if err := http.NewResponseController(writer).Flush(); err != nil {
			t.Error(err)
			return
		}
		select {
		case <-time.After(1200 * time.Millisecond):
			fmt.Fprint(writer, "second-frame")
		case <-request.Context().Done():
			t.Error("camera stream ended at the buffered-response timeout")
		}
	}))
	t.Cleanup(upstream.Close)
	proxy := startAuthenticatedProxy(t, upstream.URL, Config{UpstreamTimeoutSeconds: 1}, nil)
	response := requestThroughProxy(t, proxy, http.MethodGet, "/camera", nil, nil)
	if body := readProxyResponse(t, response); response.StatusCode != http.StatusOK || body != "first-framesecond-frame" {
		t.Fatalf("camera stream: status=%d body=%q", response.StatusCode, body)
	}
}

func TestProxyPreservesWildcardSeparator(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fmt.Fprint(writer, request.RequestURI)
	}))
	t.Cleanup(upstream.Close)
	proxy := startAuthenticatedProxy(t, upstream.URL+"/base", Config{}, nil)
	for _, test := range []struct { path, expected string }{
		{"/", "/base/"},
		{"/%2Ffoo", "/base/%2Ffoo"},
	} {
		response := requestThroughProxy(t, proxy, http.MethodGet, test.path, nil, nil)
		if body := readProxyResponse(t, response); response.StatusCode != http.StatusOK || body != test.expected {
			t.Errorf("path %q: status=%d upstream URI=%q; expected %q", test.path, response.StatusCode, body, test.expected)
		}
	}
}

func TestProxyLogsCompletedCameraStreams(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
				writer.WriteHeader(http.StatusAccepted)
				fmt.Fprint(writer, "camera-frame")
			}))
			t.Cleanup(upstream.Close)
			proxy := startAuthenticatedProxy(t, upstream.URL, Config{}, nil)
			logFile := accessLog.(*os.File)
			if !enabled {
				accessLog = nil
			}
			response := requestThroughProxy(t, proxy, http.MethodGet, "/camera", nil, nil)
			if body := readProxyResponse(t, response); response.StatusCode != http.StatusAccepted || body != "camera-frame" {
				t.Fatalf("stream: status=%d body=%q", response.StatusCode, body)
			}
			recorded, err := os.ReadFile(logFile.Name())
			if err != nil {
				t.Fatal(err)
			}
			if enabled && !strings.Contains(string(recorded), " 202 - ") {
				t.Fatalf("stream completion log is missing upstream status: %q", recorded)
			}
			if !enabled && len(recorded) != 0 {
				t.Fatalf("disabled access log received stream output: %q", recorded)
			}
		})
	}
}
