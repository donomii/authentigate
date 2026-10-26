package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func upgradeAndHandle(c *Context, req *http.Request) {
	if !sameWebsocketOrigin(c.Request) {
		c.String(http.StatusForbidden, "WebSocket origin must match %s", c.Request.Host)
		return
	}
	target := *req.URL
	if target.Scheme == "https" {
		target.Scheme = "wss"
	} else {
		target.Scheme = "ws"
	}
	headers := req.Header.Clone()
	for _, name := range []string{"Sec-WebSocket-Key", "Sec-WebSocket-Version", "Sec-WebSocket-Extensions", "Sec-WebSocket-Protocol"} {
		headers.Del(name)
	}
	dialer := websocket.Dialer{
		HandshakeTimeout: relayTimeout(),
		Subprotocols: websocket.Subprotocols(c.Request),
	}
	socket, response, err := dialer.DialContext(c.Request.Context(), target.String(), headers)
	if err != nil {
		if response != nil {
			if response.Body != nil {
				response.Body.Close()
			}
			err = fmt.Errorf("handshake returned %s: %w", response.Status, err)
		}
		logWebsocketFailure(req.URL.Host, err)
		c.String(http.StatusBadGateway, "WebSocket connection to upstream %s failed: %v", req.URL.Host, upstreamErrorCause(err))
		return
	}
	defer socket.Close()
	socket.SetReadLimit(relayBodyLimit())

	upgrader := upGrader
	if protocol := socket.Subprotocol(); protocol != "" {
		upgrader.Subprotocols = []string{protocol}
	}
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logWebsocketFailure(req.URL.Host, err)
		return
	}
	defer ws.Close()
	ws.SetReadLimit(relayBodyLimit())

	go relayWebsocketMessages(ws, socket)
	relayWebsocketMessages(socket, ws)
}

func relayWebsocketMessages(source, destination *websocket.Conn) {
	defer source.Close()
	defer destination.Close()
	for {
		//Read data in ws
		messageType, message, err := source.ReadMessage()
		if err != nil {
			return
		}
		destination.SetWriteDeadline(time.Now().Add(relayTimeout()))
		if err := destination.WriteMessage(messageType, message); err != nil {
			return
		}
	}
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
