package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func newWebSocketRequest(c *Context, target string) (*http.Request, error) {
	upstream, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	api := c.Param("api")
	if !strings.HasSuffix(c.Request.URL.Path, api) {
		return nil, fmt.Errorf("WebSocket route suffix does not match the request path")
	}
	escapedPath := c.Request.URL.EscapedPath()
	prefixBytes := len(c.Request.URL.Path) - len(api)
	rawOffset := 0
	for i := 0; i < prefixBytes; i++ {
		if escapedPath[rawOffset] == '%' {
			rawOffset += 3
		} else {
			rawOffset++
		}
	}
	upstream.RawPath = upstream.EscapedPath() + "/" + escapedPath[rawOffset:]
	upstream.Path += "/" + api
	if upstream.RawQuery != "" && c.Request.URL.RawQuery != "" {
		upstream.RawQuery += "&" + c.Request.URL.RawQuery
	} else if c.Request.URL.RawQuery != "" {
		upstream.RawQuery = c.Request.URL.RawQuery
	}
	upstream.ForceQuery = upstream.ForceQuery || c.Request.URL.ForceQuery
	return http.NewRequestWithContext(c.Request.Context(), http.MethodGet, upstream.String(), nil)
}

func upgradeAndHandle(c *Context, req *http.Request) {
	if !sameWebsocketOrigin(c.Request) {
		c.String(http.StatusForbidden, "WebSocket origin must match %s", c.Request.Host)
		return
	}
	socket, response, err := websocketClientConn(req, websocket.Subprotocols(c.Request))
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		logWebSocketUpstreamFailure(req.URL.Host, status, err)
		c.String(http.StatusBadGateway, "WebSocket connection to upstream %s failed: %v", req.URL.Host, upstreamErrorCause(err))
		return
	}
	defer socket.Close()
	socket.SetReadLimit(relayBodyLimit())

	upgrader := upGrader
	upgrader.Subprotocols = nil
	if protocol := socket.Subprotocol(); protocol != "" {
		upgrader.Subprotocols = []string{protocol}
	}
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logWebSocketClientFailure(req.URL.Host, err)
		return
	}
	defer ws.Close()
	ws.SetReadLimit(relayBodyLimit())

	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		defer ws.Close()
		defer socket.Close()
		relayWebSocketMessages(ws, socket)
	}()
	relayWebSocketMessages(socket, ws)
	ws.Close()
	socket.Close()
	<-clientDone
}

func relayWebSocketMessages(source, destination *websocket.Conn) {
	for {
		//Read data in ws
		messageType, reader, err := source.NextReader()
		if err != nil {
			return
		}
		destination.SetWriteDeadline(time.Now().Add(relayTimeout()))
		writer, err := destination.NextWriter(messageType)
		if err != nil {
			return
		}
		_, err = io.Copy(writer, reader)
		closeErr := writer.Close()
		if err != nil || closeErr != nil {
			return
		}
	}
}

func websocketClientConn(req *http.Request, protocols []string) (*websocket.Conn, *http.Response, error) {
	upstream := *req.URL
	switch upstream.Scheme {
	case "http":
		upstream.Scheme = "ws"
	case "https":
		upstream.Scheme = "wss"
	case "ws", "wss":
	default:
		return nil, nil, fmt.Errorf("unsupported WebSocket upstream scheme %q", upstream.Scheme)
	}
	headers := req.Header.Clone()
	for _, header := range []string{
		"Connection", "Upgrade", "Sec-Websocket-Key", "Sec-Websocket-Version",
		"Sec-Websocket-Extensions", "Sec-Websocket-Protocol",
	} {
		headers.Del(header)
	}
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = relayTimeout()
	dialer.Subprotocols = protocols
	return dialer.DialContext(req.Context(), upstream.String(), headers)
}

func sameWebsocketOrigin(request *http.Request) bool {
	origins := request.Header.Values("Origin")
	if len(origins) == 0 {
		return true
	}
	if len(origins) != 1 {
		return false
	}
	origin, err := http.NewRequest(http.MethodGet, origins[0], nil)
	return err == nil && (origin.URL.Scheme == "http" || origin.URL.Scheme == "https") && strings.EqualFold(origin.URL.Host, request.Host)
}
