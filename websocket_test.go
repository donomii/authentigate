package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebsocketForwardsIdentityQuerySubprotocolAndMessages(t *testing.T) {
	receivedRequest := make(chan *http.Request, 1)
	upstreamFinished := make(chan struct{})
	var proxyOrigin string
	upgrader := websocket.Upgrader{
		Subprotocols: []string{"chat.v2"},
		CheckOrigin:  func(request *http.Request) bool { return request.Header.Get("Origin") == proxyOrigin },
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer close(upstreamFinished)
		receivedRequest <- request.Clone(request.Context())
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		for {
			messageType, payload, err := connection.ReadMessage()
			if err != nil {
				return
			}
			if err := connection.WriteMessage(messageType, payload); err != nil {
				return
			}
		}
	}))
	t.Cleanup(upstream.Close)
	allowed := []string{"Origin", "Authorization", "X-Pass", "Authentigate-Id", "Sec-WebSocket-Protocol"}
	proxy := startAuthenticatedProxy(t, upstream.URL+"/socket?fixed=1", Config{}, allowed)
	proxyOrigin = proxy.URL
	dialer := websocket.Dialer{Subprotocols: []string{"chat.v1", "chat.v2"}, HandshakeTimeout: 3 * time.Second}
	headers := http.Header{
		"Origin": {proxy.URL}, "Authorization": {"Bearer websocket-auth"}, "X-Pass": {"websocket-header"},
		"Authentigate-Id": {"forged"},
	}
	address := "ws" + strings.TrimPrefix(proxy.URL, "http") + relayTestPath + "/room%2Fone?topic=a%2Fb"
	connection, response, err := dialer.Dial(address, headers)
	if err != nil {
		if response != nil {
			response.Body.Close()
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		connection.Close()
		select {
		case <-upstreamFinished:
		case <-time.After(3 * time.Second):
			t.Error("upstream WebSocket remained open after client closed")
		}
	})
	if protocol := connection.Subprotocol(); protocol != "chat.v2" {
		t.Errorf("negotiated protocol: got %q, expected chat.v2", protocol)
	}
	request := <-receivedRequest
	if request.RequestURI != "/socket/room%2Fone?fixed=1&topic=a%2Fb" {
		t.Errorf("upstream URI: %q", request.RequestURI)
	}
	for name, expected := range map[string]string{
		"Authentigate-Id": relayTestUserID, "Authentigate-Token": relayTestToken,
		"Authorization": "Bearer websocket-auth", "X-Pass": "websocket-header", "Origin": proxy.URL,
	} {
		if received := request.Header.Get(name); received != expected {
			t.Errorf("%s: got %q, expected %q", name, received, expected)
		}
	}
	for _, messageType := range []int{websocket.TextMessage, websocket.BinaryMessage} {
		payload := []byte{0, 1, 2, 'r', 'e', 'l', 'a', 'y'}
		if err := connection.WriteMessage(messageType, payload); err != nil {
			t.Fatal(err)
		}
		if err := connection.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		receivedType, receivedPayload, err := connection.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if receivedType != messageType || !bytes.Equal(receivedPayload, payload) {
			t.Errorf("received type=%d payload=%v; expected type=%d payload=%v", receivedType, receivedPayload, messageType, payload)
		}
	}
}

func TestWebsocketRejectsCrossOriginBeforeConnectingUpstream(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upstreamCalls.Add(1)
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)
	proxy := startAuthenticatedProxy(t, upstream.URL, Config{}, nil)
	address := "ws" + strings.TrimPrefix(proxy.URL, "http") + relayTestPath + "/socket"
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	for _, origins := range [][]string{{"http://attacker.invalid"}, {"null"}, {proxy.URL, "http://attacker.invalid"}} {
		connection, response, err := dialer.Dial(address, http.Header{"Origin": origins})
		if connection != nil {
			connection.Close()
			t.Error("cross-origin WebSocket unexpectedly upgraded")
		}
		if response == nil {
			t.Fatalf("origin %v: no rejection response: %v", origins, err)
		}
		body := readProxyResponse(t, response)
		if response.StatusCode != http.StatusForbidden {
			t.Errorf("origin %v: received %d %q, expected 403", origins, response.StatusCode, body)
		}
	}
	if calls := upstreamCalls.Load(); calls != 0 {
		t.Errorf("denied WebSocket requests reached upstream %d times", calls)
	}
}

func TestWebsocketUpstreamFailureRemainsHTTPError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)
	proxy := startAuthenticatedProxy(t, upstream.URL, Config{}, nil)
	address := "ws" + strings.TrimPrefix(proxy.URL, "http") + relayTestPath + "/socket"
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	connection, response, err := dialer.Dial(address, http.Header{"Origin": {proxy.URL}})
	if connection != nil {
		connection.Close()
		t.Fatal("client connection upgraded despite upstream rejection")
	}
	if err == nil || response == nil {
		t.Fatalf("expected failed upgrade with HTTP response, received response=%v error=%v", response, err)
	}
	body := readProxyResponse(t, response)
	if response.StatusCode != http.StatusBadGateway || !strings.Contains(body, "WebSocket connection") {
		t.Errorf("upstream rejection: received %d %q, expected 502", response.StatusCode, body)
	}
}
