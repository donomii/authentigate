package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// NewError example
func NewError(ctx *gin.Context, status int, err string) {
	er := HTTPError{
		Code:    status,
		Message: err,
	}
	ctx.JSON(status, er)
}

// HTTPError example
type HTTPError struct {
	Code    int    `json:"code" example:"400"`
	Message string `json:"message" example:"status bad request"`
}

func makeAuthed(handlerFunc func(*gin.Context, string, string)) func(c *gin.Context) {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logShopFailure(c, fmt.Sprint(r))
				NewError(c, http.StatusInternalServerError, "Handler failure!")
				return
			}
		}()
		id := c.Request.Header.Get("authentigate-id")
		if id == "" {
			id = "demoUser"
		}
		baseUrl := c.Request.Header.Get("authentigate-base-url")
		logShopUser(id)
		handlerFunc(c, id, baseUrl)
	}
}
