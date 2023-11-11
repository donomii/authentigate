package main
import (

	"log"
	"net/http"
	"net/url"
	"github.com/gorilla/websocket"
	"github.com/donomii/gin"
)
func upgradeAndHandle(c *gin.Context, req *http.Request) {
	ws, err := upGrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println("error get connection")
		panic(err)
	}
	defer ws.Close()

	socket := websocketClientConn(req.URL.Host, req.URL.Path)

	defer socket.Close()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Socket closed while processing %v: %v", c.Request.URL, r)
			}
		}()

		defer ws.Close()
		defer socket.Close()
		for {
			//Read data in ws
			mt, message, err := ws.ReadMessage()
			if err != nil {
				log.Println("error reading message from client")
				panic(err)
			}

			log.Printf("!!!!!Message %+v\nmt %+v\n", message, mt)

			err = socket.WriteMessage(mt, message)
			if err != nil {
				log.Println("error writing message to server: " + err.Error())
				panic(err)
			}

		}
	}()

	for {
		//Read data in ws
		mt, message, err := socket.ReadMessage()
		if err != nil {
			log.Println("error read message")
			panic(err)
		}

		log.Printf("Message %+v\nmt %+v\n", message, mt)
		err = ws.WriteMessage(mt, message)
		if err != nil {
			log.Println("error write message: " + err.Error())
			panic(err)
		}

	}

}

func websocketClientConn(addr, path string) *websocket.Conn {

	u := url.URL{Scheme: "ws", Host: addr, Path: path}
	log.Printf("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		panic(err)
	}
	return c
}