package main

import (
	"fmt"
	"log"
)

func logMissingRoute(context *MyContext) {
	log.Printf("No route found for host: %s, method: %s", context.Request.Host, context.Request.Method)
}

func logInvalidTarget(target string, err error) {
	log.Printf("Invalid upstream URL %q: %v", target, err)
}

func logUpstreamError(target, method string, err error) {
	log.Printf("Upstream %s request to %q failed: %v", method, target, err)
}

func logAccess(context *MyContext, status, size int) {
	if accessLog != nil {
		if err := accessLog.Output(2, format_clf(context, "", fmt.Sprint(status), fmt.Sprint(size))); err != nil {
			logAccessError(err)
		}
	}
}

func logAccessError(err error) {
	log.Printf("Write access log failed: %v", err)
}
