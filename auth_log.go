package main

import (
	"log"
	"net/url"
	"regexp"
	"runtime/debug"
	"strings"
)

var logURLPattern = regexp.MustCompile(`(?i)\b(?:https?|wss?|otpauth)://[^\s"'<>]+`)
var logCredentialPattern = regexp.MustCompile(`(?i)("?(?:access_token|refresh_token|id_token|client_secret|otp_secret|authentigate-token|token|secret|password)"?\s*[:=]\s*)("[^"]*"|[^\s&;,}]+)`)

func redactLogSecrets(message string, secrets ...string) string {
	message = logURLPattern.ReplaceAllString(message, "[redacted URL]")
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
			message = strings.ReplaceAll(message, url.QueryEscape(secret), "[redacted]")
			message = strings.ReplaceAll(message, url.PathEscape(secret), "[redacted]")
		}
	}
	message = logURLPattern.ReplaceAllString(message, "[redacted URL]")
	return logCredentialPattern.ReplaceAllString(message, "${1}[redacted]")
}

func requestLogDetail(c *Context, detail string) string {
	secrets := []string{c.Param("token"), c.Request.URL.String(), c.Request.RequestURI}
	for _, values := range c.Request.Header {
		secrets = append(secrets, values...)
	}
	for _, values := range c.Request.URL.Query() {
		secrets = append(secrets, values...)
	}
	for _, values := range c.Request.Form {
		secrets = append(secrets, values...)
	}
	for _, cookie := range c.Request.Cookies() {
		secrets = append(secrets, cookie.Value)
	}
	return redactLogSecrets(detail, secrets...)
}

func requestLogRoute(c *Context) string {
	if c.Request.Pattern != "" {
		return c.Request.Pattern
	}
	return "[unmatched route]"
}

func logRequestFailure(c *Context, failure string) {
	log.Printf("Request failed: method=%q route=%q error=%q\n%s", c.Request.Method, requestLogRoute(c), requestLogDetail(c, failure), debug.Stack())
}

func logSessionRejected(c *Context) {
	log.Printf("Session rejected: method=%q route=%q", c.Request.Method, requestLogRoute(c))
}

func logUserSaved(id string) {
	log.Printf("Saved user: id=%q", id)
}

func logTokenRotated(id string) {
	log.Printf("Rotated session token: user=%q", id)
}

func logTokenCleanupFailure(id, oldToken, newToken string, err error) {
	log.Printf("Failed to remove revoked token mapping: user=%q error=%q", id, redactLogSecrets(err.Error(), oldToken, newToken))
}

func logRelayRequest(c *Context, id string) {
	log.Printf("Relaying request: user=%q method=%q route=%q", id, c.Request.Method, requestLogRoute(c))
}

func logRelayFailure(c *Context, err error) {
	log.Printf("Upstream request failed: method=%q route=%q error=%q", c.Request.Method, requestLogRoute(c), requestLogDetail(c, err.Error()))
}

func logStreamReadFailure(c *Context, err error) {
	log.Printf("Upstream stream read failed: route=%q error=%q", requestLogRoute(c), requestLogDetail(c, err.Error()))
}

func logStreamWriteFailure(c *Context, err error) {
	log.Printf("Client stream write failed: route=%q error=%q", requestLogRoute(c), requestLogDetail(c, err.Error()))
}

func logOAuthFailure(c *Context, err error) {
	detail := requestLogDetail(c, err.Error())
	for _, data := range providerSecrets {
		detail = redactLogSecrets(detail, data["clientSecret"])
	}
	log.Printf("OAuth request failed: provider=%q error=%q", c.Param("provider"), detail)
}

func logOTPVerification(ok bool) {
	log.Printf("OTP verification succeeded: %t", ok)
}

func logConfiguredRoute(relay Redirect) {
	target, err := url.Parse(relay.To)
	if err != nil {
		log.Printf("Configured relay: name=%q method=%q upstream URL is invalid", relay.Name, relay.Tipe)
		return
	}
	log.Printf("Configured relay: name=%q method=%q upstream=%q", relay.Name, relay.Tipe, target.Host)
}
