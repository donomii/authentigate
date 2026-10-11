package main

import "log"

func logPresenceFailure(hostname string, err error) {
	log.Printf("Could not update presence for host %q: %v", hostname, err)
}

func logPresenceStatus(hostname string, status int) {
	log.Printf("Could not update presence for host %q: server returned HTTP %d, expected HTTP 200", hostname, status)
}
