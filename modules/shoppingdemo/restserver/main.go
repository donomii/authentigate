package main

import (
	"os"

	"github.com/gin-gonic/gin"
)

// @title Shop Demo
// @version 1.0
// @description Example fruit store
// @termsOfService https://www.gnu.org/licenses/agpl-3.0.en.html
// @contact.name Jeremy Price
// @contact.url http://praeceptamachinae.com
// @contact.email jeremy@praeceptamachinae.com
// @license.name Affero GPL
// @license.url https://www.gnu.org/licenses/agpl-3.0.en.html
// @host https://entirety.praeceptamachinae.com/
// @BasePath /api/v1
func main() {
	os.Mkdir("userdata", 0700)
	router := gin.Default()
	configureRoutes(router, "/shoppr/api/v1/")
	router.Run("127.0.0.1:8098")
}
