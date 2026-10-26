package main

import (
	"log"
	"net/url"
	"strings"
)

func logAuthFailure(c *Context, detail string) {
	log.Printf("Authentication request failed: method=%s route=%s error=%s", c.Request.Method, c.Request.Pattern, authErrorDetail(c, detail))
}

func authErrorDetail(c *Context, detail string) string {
	secrets := []string{c.Param("token"), c.Query("state"), c.Query("code")}
	for _, cookie := range c.Request.Cookies() {
		secrets = append(secrets, cookie.Value)
	}
	for _, provider := range providerSecrets {
		secrets = append(secrets, provider["clientSecret"])
	}
	for _, secret := range secrets {
		if secret != "" {
			detail = strings.ReplaceAll(detail, secret, "[redacted]")
			detail = strings.ReplaceAll(detail, url.QueryEscape(secret), "[redacted]")
			detail = strings.ReplaceAll(detail, url.PathEscape(secret), "[redacted]")
		}
	}
	detail = strings.ReplaceAll(detail, "\n", "\\n")
	return detail
}

func logTokenCleanupFailure(id, token string, err error) {
	detail := strings.ReplaceAll(err.Error(), token, "[redacted]")
	log.Printf("Session token revoked for user %s, but obsolete index cleanup failed: %s", id, detail)
}

func logUsernameCleanupFailure(id, username string, err error) {
	log.Printf("Username changed for user %s, but obsolete username %q index cleanup failed: %v", id, username, err)
}
