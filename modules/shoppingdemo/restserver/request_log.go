package main

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/gin-gonic/gin"
)

var shopLogURL = regexp.MustCompile(`(?i)\bhttps?://[^\s"'<>]+`)

func shopRequestLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if failure := recover(); failure != nil {
				logShopFailure(c, fmt.Sprint(failure))
				c.AbortWithStatus(http.StatusInternalServerError)
			}
			logShopResponse(c)
		}()
		c.Next()
	}
}

func logShopUser(id string) {
	log.Printf("Shop request: user=%q", id)
}

func logShopResponse(c *gin.Context) {
	log.Printf("Shop response: method=%q route=%q status=%d", c.Request.Method, c.FullPath(), c.Writer.Status())
}

func logShopFailure(c *gin.Context, detail string) {
	detail = shopLogURL.ReplaceAllString(detail, "[redacted URL]")
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
	log.Printf("Shop request failed: method=%q route=%q error=%q\n%s", c.Request.Method, c.FullPath(), detail, debug.Stack())
}
