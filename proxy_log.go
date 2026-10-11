package main

import "log"

func logUpstreamFailure(route, host string, err error) {
	log.Printf("Upstream request failed: route=%q host=%q error=%v", route, host, err)
}

func logWebsocketFailure(host string, err error) {
	log.Printf("WebSocket relay failed: upstream=%q error=%v", host, err)
}
