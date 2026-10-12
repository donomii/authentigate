package main

import (
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

var requestLogURL = regexp.MustCompile(`(?i)\bhttps?://[^\s"'<>]+`)
var accessLogMutex sync.Mutex

func writeAccessLog(c *MyContext, status, size string) {
	accessLogMutex.Lock()
	defer accessLogMutex.Unlock()
	if _, err := accessLog.WriteString(format_clf(c, "", status, size) + "\n"); err != nil {
		panic(fmt.Errorf("write proxy access log: %w", err))
	}
	if err := accessLog.Flush(); err != nil {
		panic(fmt.Errorf("flush proxy access log: %w", err))
	}
}

func upstreamLogHost(target string) string {
	parsed, err := url.Parse(target)
	if err != nil {
		return "[invalid URL]"
	}
	return parsed.Host
}

func logConfiguredRoute(index int, relay Redirect) {
	log.Printf("Configured route: index=%d host=%q upstream=%q name=%q", index, relay.Host, upstreamLogHost(relay.To), relay.Name)
}

func logMatchedRoute(method string, relay Redirect) {
	log.Printf("Matched route: method=%q host=%q upstream=%q", method, relay.Host, upstreamLogHost(relay.To))
}

func logProxyFailure(c *MyContext, detail string) {
	detail = requestLogURL.ReplaceAllString(detail, "[redacted URL]")
	secrets := []string{c.Request.URL.String(), c.Request.RequestURI, c.Request.URL.Path}
	for _, values := range c.Request.Header {
		secrets = append(secrets, values...)
	}
	for _, cookie := range c.Request.Cookies() {
		secrets = append(secrets, cookie.Value)
	}
	for _, values := range c.Request.URL.Query() {
		secrets = append(secrets, values...)
	}
	for _, secret := range secrets {
		if secret != "" {
			detail = strings.ReplaceAll(detail, secret, "[redacted]")
		}
	}
	log.Printf("Proxy request failed: method=%q host=%q error=%q", c.Request.Method, c.Request.Host, detail)
}
