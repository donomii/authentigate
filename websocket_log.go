package main

import (
	"errors"
	"log"
	"net/url"
)

func logWebSocketUpstreamFailure(host string, status int, err error) {
	var requestError *url.Error
	if errors.As(err, &requestError) {
		err = requestError.Err
	}
	log.Printf("WebSocket connection to upstream %q failed (HTTP status %d; 0 means no response): %v", host, status, err)
}

func logWebSocketClientFailure(host string, err error) {
	log.Printf("WebSocket client handshake for upstream %q failed: %v", host, err)
}
