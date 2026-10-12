package main

import (
	"log"
	"regexp"
	"strings"
)

var presenceLogURL = regexp.MustCompile(`(?i)\bhttps?://[^\s"'<>]+`)

func logPresenceRequest(hostname string) {
	log.Printf("Reporting presence: host=%q", hostname)
}

func logPresenceFailure(hostname, token, detail string) {
	detail = presenceLogURL.ReplaceAllString(detail, "[redacted URL]")
	if token != "" {
		detail = strings.ReplaceAll(detail, token, "[redacted]")
	}
	log.Printf("Presence report failed: host=%q error=%q", hostname, detail)
}
