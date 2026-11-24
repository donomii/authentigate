package main

import (
	"log"
	"runtime/debug"
)

func logHandlerFailure(failure string) {
	log.Printf("Shop handler failed: %s\n%s", failure, debug.Stack())
}

func logUserRequest(id string) {
	log.Printf("Shop request for user %q", id)
}
