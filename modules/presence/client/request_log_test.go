package main

import (
	"bytes"
	"errors"
	"log"
	"net/url"
	"strings"
	"testing"
)

func TestPresenceFailureLogsKeepCauseWithoutToken(t *testing.T) {
	var logs bytes.Buffer
	originalLog := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(originalLog) })
	err := &url.Error{Op: "Get", URL: "https://gateway.example/secure/private-session/presence?code=private-code", Err: errors.New("connection refused")}
	logPresenceRequest("local-computer")
	logPresenceFailure("local-computer", "private-session", err.Error())
	if strings.Contains(logs.String(), "private-") {
		t.Fatalf("presence logs contain credentials: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "local-computer") || !strings.Contains(logs.String(), "connection refused") {
		t.Fatalf("presence logs omitted host or failure cause: %s", logs.String())
	}
}
