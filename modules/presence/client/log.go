package main

import "log"

func logPresenceStatus(hostname string, status int) {
	log.Printf("Could not update presence for host %q: server returned HTTP %d, expected HTTP 200", hostname, status)
}
