package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/gin-gonic/gin"
)

var formSecret = newFormSecret()

func newFormSecret() []byte {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		panic(fmt.Errorf("could not initialize shop form protection: %w", err))
	}
	return secret
}

func formToken(c *gin.Context) string {
	signature := hmac.New(sha256.New, formSecret)
	signature.Write([]byte(c.GetHeader("authentigate-id")))
	signature.Write([]byte{0})
	signature.Write([]byte(c.GetHeader("authentigate-token")))
	return hex.EncodeToString(signature.Sum(nil))
}

func formField(c *gin.Context) string {
	return `<input type="hidden" name="csrf" value="` + formToken(c) + `">`
}
