package main

import (
	"fmt"
	"log"
	"net/http"
	"runtime"

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
				log.Println("Caught panic in Authhandler", r)
				buf := make([]byte, 1<<16)
				runtime.Stack(buf, true)
				log.Printf("%s", buf)
				NewError(c, http.StatusInternalServerError, "Handler failure!")
				return
			}
		}()
		id := c.Request.Header.Get("authentigate-id")
		fmt.Printf("Request headers: %+v\n", c.Request.Header)
		if id == "" {
			id = "demoUser"
		}
		baseUrl := c.Request.Header.Get("authentigate-base-url")
		log.Printf("Got real user id: '%v'", id)
		handlerFunc(c, id, baseUrl)
	}
}
