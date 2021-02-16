package main

import (
	"os"

	"github.com/gin-gonic/gin"
)

func main() {
	os.Mkdir("userdata", 0700)
	router := gin.Default()
	configureRoutes(router, "/shoppr/")
	router.Run("127.0.0.1:98")
}
