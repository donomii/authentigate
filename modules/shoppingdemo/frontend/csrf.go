package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"

	"github.com/gin-gonic/gin"
)

func formToken(c *gin.Context) string {
	sessionToken := c.GetHeader("authentigate-token")
	if sessionToken == "" {
		return ""
	}
	signature := hmac.New(sha256.New, []byte(sessionToken))
	signature.Write([]byte("authentigate-shop-form\x00"))
	signature.Write([]byte(c.GetHeader("authentigate-id")))
	return hex.EncodeToString(signature.Sum(nil))
}

func formField(c *gin.Context) string {
	return `<input type="hidden" name="csrf" value="` + formToken(c) + `">`
}
