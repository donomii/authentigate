package main

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/gin-gonic/gin"
	"gopkg.in/resty.v1"
)

var frontendLogURL = regexp.MustCompile(`(?i)\bhttps?://[^\s"'<>]+`)

func logBackendResponse(resp *resty.Response) {
	if resp == nil {
		log.Print("Shop backend response missing")
		return
	}
	log.Printf("Shop backend response: status=%d elapsed=%s received=%s", resp.StatusCode(), resp.Time(), resp.ReceivedAt())
}

func frontendRequestLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if failure := recover(); failure != nil {
				logFrontendFailure(c, fmt.Sprint(failure))
				c.AbortWithStatus(http.StatusInternalServerError)
			}
			logFrontendResponse(c)
		}()
		c.Next()
	}
}

func logFrontendResponse(c *gin.Context) {
	log.Printf("Shop frontend response: method=%q route=%q status=%d", c.Request.Method, c.FullPath(), c.Writer.Status())
}

func logFrontendFailure(c *gin.Context, detail string) {
	detail = frontendLogURL.ReplaceAllString(detail, "[redacted URL]")
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
	log.Printf("Shop frontend request failed: method=%q route=%q error=%q\n%s", c.Request.Method, c.FullPath(), detail, debug.Stack())
}
