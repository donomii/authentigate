package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketRelayRequestAndMessages(t *testing.T) {
	for _, secure := range []bool{false, true} {
		name := "HTTP"
		if secure {
			name = "HTTPS"
		}
		t.Run(name, func(t *testing.T) {
			requests := make(chan *http.Request, 1)
			upstreamDone := make(chan struct{})
			upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(upstreamDone)
				requests <- r
				upgrader := websocket.Upgrader{Subprotocols: []string{"chat.v2"}}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Errorf("upstream upgrade: %v", err)
					return
				}
				defer conn.Close()
				for {
					kind, message, err := conn.ReadMessage()
					if err != nil {
						return
					}
					if err := conn.WriteMessage(kind, message); err != nil {
						return
					}
				}
			}))
			if secure {
				upstream.StartTLS()
				roots := x509.NewCertPool()
				roots.AddCert(upstream.Certificate())
				dialer := *websocket.DefaultDialer
				dialer.TLSClientConfig = &tls.Config{RootCAs: roots}
				originalDialer := websocket.DefaultDialer
				websocket.DefaultDialer = &dialer
				defer func() { websocket.DefaultDialer = originalDialer }()
			} else {
				upstream.Start()
			}
			defer upstream.Close()
			gateway := websocketGateway(t, upstream.URL+"/base%2Fdir?configured=a%2Fb")
			defer gateway.Close()

			dialer := websocket.Dialer{Subprotocols: []string{"chat.v1", "chat.v2"}, HandshakeTimeout: 5 * time.Second}
			client, response, err := dialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+
				"/secure/token/svc/first%2Fpart/%3F%23%25?filter=x%2Fy&filter=z", nil)
			if err != nil {
				t.Fatalf("gateway upgrade: %v (response %v)", err, response)
			}
			defer client.Close()
			if got := client.Subprotocol(); got != "chat.v2" {
				t.Fatalf("selected subprotocol = %q; want chat.v2", got)
			}
			request := <-requests
			wantURI := "/base%2Fdir/first%2Fpart/%3F%23%25?configured=a%2Fb&filter=x%2Fy&filter=z"
			if request.RequestURI != wantURI {
				t.Errorf("upstream URI = %q; want %q", request.RequestURI, wantURI)
			}
			for key, value := range map[string]string{
				"authentigate-id": "user-123", "authentigate-token": "trusted-token",
				"authentigate-base-url": "https://gateway.example/secure/c/svc/",
				"X-Request-ID":          "request-456", "Sec-Websocket-Protocol": "chat.v1, chat.v2",
			} {
				if got := request.Header.Get(key); got != value {
					t.Errorf("upstream header %s = %q; want %q", key, got, value)
				}
			}
			if got := request.TLS != nil; got != secure {
				t.Errorf("upstream TLS = %v; want %v", got, secure)
			}
			for _, kind := range []int{websocket.TextMessage, websocket.BinaryMessage} {
				message := bytes.Repeat([]byte("message payload"), 1024)
				if err := client.WriteMessage(kind, message); err != nil {
					t.Fatalf("write client message: %v", err)
				}
				client.SetReadDeadline(time.Now().Add(5 * time.Second))
				gotKind, gotMessage, err := client.ReadMessage()
				if err != nil {
					t.Fatalf("read relayed message: %v", err)
				}
				if gotKind != kind || !bytes.Equal(gotMessage, message) {
					t.Fatalf("relayed message mismatch: type %d, length %d", gotKind, len(gotMessage))
				}
			}
			client.Close()
			select {
			case <-upstreamDone:
			case <-time.After(5 * time.Second):
				t.Fatal("upstream connection remained open after client disconnected")
			}
		})
	}
}

func TestWebSocketUpstreamFailureBeforeClientUpgrade(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "access denied with private upstream detail", http.StatusForbidden)
	}))
	defer upstream.Close()
	var logs bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(originalOutput)
	gateway := websocketGateway(t, upstream.URL)
	defer gateway.Close()
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	client, response, err := dialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+
		"/secure/token/svc/connect?secret=private-query", nil)
	if client != nil {
		client.Close()
		t.Fatal("gateway accepted client before upstream failed")
	}
	if err == nil || response == nil || response.StatusCode != http.StatusBadGateway {
		t.Fatalf("upstream refusal returned response %v, error %v; want HTTP 502", response, err)
	}
	response.Body.Close()
	if !strings.Contains(logs.String(), "HTTP status 403") {
		t.Errorf("upstream refusal log lacks status: %s", logs.String())
	}
	for _, secret := range []string{"private-query", "trusted-token", "private upstream detail"} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("upstream refusal log contains private value %q", secret)
		}
	}
}

func TestWebSocketRequestEscapedRoutePrefix(t *testing.T) {
	for _, test := range []struct {
		path string
		api  string
		want string
	}{
		{"/secure/t%2Foken/svc/file%2Fname?x=%23", "file/name", "https://upstream/base%2Fpath/file%2Fname?configured=yes&x=%23"},
		{"/secure/token/svc/", "", "https://upstream/base%2Fpath/?configured=yes"},
		{"/secure/token/svc/caf%C3%A9", "café", "https://upstream/base%2Fpath/caf%C3%A9?configured=yes"},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.SetPathValue("api", test.api)
		upstream, err := newWebSocketRequest(&Context{Request: request}, "https://upstream/base%2Fpath?configured=yes")
		if err != nil {
			t.Fatalf("construct upstream request: %v", err)
		}
		if got := upstream.URL.String(); got != test.want {
			t.Errorf("upstream URL = %q; want %q", got, test.want)
		}
	}
}

func websocketGateway(t *testing.T, upstream string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /secure/{token}/svc/{api...}", func(w http.ResponseWriter, r *http.Request) {
		c := &Context{Writer: w, Request: r}
		request, err := newWebSocketRequest(c, upstream)
		if err != nil {
			t.Errorf("construct upstream request: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		AddAuthToRequest(request, "user-123", "trusted-token", "https://gateway.example/secure/", &Redirect{Name: "svc"}, true)
		request.Header.Set("X-Request-ID", "request-456")
		for _, header := range []string{"Connection", "Upgrade", "Sec-Websocket-Key", "Sec-Websocket-Version", "Sec-Websocket-Protocol"} {
			request.Header.Set(header, r.Header.Get(header))
		}
		upgradeAndHandle(c, request)
	})
	return httptest.NewServer(mux)
}

func TestAuthenticatedWebSocketRelay(t *testing.T) {
	useAuthTestStore(t)
	user := createAuthTestUser(t)
	originalConfig, originalAccess := config, accessLog
	config, accessLog = &Config{Port: 8000}, io.Discard
	t.Cleanup(func() { config, accessLog = originalConfig, originalAccess })
	requests := make(chan *http.Request, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r
		upgrader := websocket.Upgrader{Subprotocols: []string{"events"}}
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upstream upgrade: %v", err)
			return
		}
		defer connection.Close()
		messageType, message, err := connection.ReadMessage()
		if err != nil {
			t.Errorf("upstream read: %v", err)
			return
		}
		if err := connection.WriteMessage(messageType, message); err != nil {
			t.Errorf("upstream write: %v", err)
		}
	}))
	defer upstream.Close()
	relay := &Redirect{Name: "socket", To: upstream.URL + "/base?fixed=1", CopyHeaders: []string{"X-Request-ID"}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /secure/{token}/socket/{api...}", makeAuthedRelay(relayGetHandler, relay, nil))
	gateway := httptest.NewServer(mux)
	defer gateway.Close()
	dialer := websocket.Dialer{Subprotocols: []string{"events"}, HandshakeTimeout: 5 * time.Second}
	endpoint := "ws" + strings.TrimPrefix(gateway.URL, "http") + "/secure/" + user.Token + "/socket/chat%2Froom?client=2"
	client, _, err := dialer.Dial(endpoint, http.Header{"X-Request-Id": []string{"request-through-auth"}})
	if err != nil {
		t.Fatalf("authenticated gateway upgrade: %v", err)
	}
	defer client.Close()
	if client.Subprotocol() != "events" {
		t.Fatalf("selected protocol=%q", client.Subprotocol())
	}
	request := <-requests
	if request.RequestURI != "/base/chat%2Froom?fixed=1&client=2" {
		t.Errorf("upstream request URI=%q", request.RequestURI)
	}
	for name, expected := range map[string]string{
		"authentigate-id": user.Id, "authentigate-token": user.Token, "X-Request-ID": "request-through-auth",
	} {
		if actual := request.Header.Get(name); actual != expected {
			t.Errorf("upstream %s=%q; expected %q", name, actual, expected)
		}
	}
	client.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := client.WriteMessage(websocket.TextMessage, []byte("authenticated message")); err != nil {
		t.Fatal(err)
	}
	_, message, err := client.ReadMessage()
	if err != nil || string(message) != "authenticated message" {
		t.Fatalf("relayed message=%q error=%v", message, err)
	}
}
