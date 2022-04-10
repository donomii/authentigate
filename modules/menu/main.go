package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/boltdb/bolt"
	"github.com/danilopolani/gocialite"
	"github.com/gin-gonic/gin"
)

var gocial = gocialite.NewDispatcher()
var sockets map[string](chan string)

type server struct {
	db *bolt.DB
}

var r *rand.Rand
var b *server

func newServer(filename string) (s *server, err error) {
	s = &server{}
	s.db, err = bolt.Open(filename, 0600, &bolt.Options{Timeout: 1 * time.Second})
	return
}

func makeAuthed(handlerFunc func(*gin.Context, string, string, string)) func(c *gin.Context) {
	return func(c *gin.Context) {
		id := c.Request.Header.Get("authentigate-id")
		baseUrl := c.Request.Header.Get("authentigate-base-url")
		topUrl := c.Request.Header.Get("authentigate-top-url")

		log.Printf("Got real user id: '%v', baseUrl: '%v'", id, baseUrl)
		handlerFunc(c, id, baseUrl, topUrl)
	}

}

func main() {
	sockets = make(map[string](chan string))
	var err error
	b, err = newServer("datastore")
	if err != nil {
		panic(err)
	}
	r = rand.New(rand.NewSource(time.Now().UnixNano()))
	router := gin.Default()

	router.GET("/token", makeAuthed(tokenShowHandler))
	router.GET("/about", makeAuthed(aboutShowHandler))
	router.GET("/api/cameraUploader", makeAuthed(cameraUploaderHandler))
	router.GET("/api/cameraWatcher", makeAuthed(cameraWatcherHandler))
	router.GET("/api/shutdown", makeAuthed(shutdownHandler))
	router.GET("/api/mainMenu", makeAuthed(mainMenuHandler))
	router.GET("/api/subMenu", makeAuthed(subMenuHandler))
	router.GET("/api/newToken", makeAuthed(newTokenHandler))
	router.GET("/api/send", makeAuthed(messageHandler))
	router.GET("/api/check", makeAuthed(checkHandler))
	router.GET("/api/image", makeAuthed(imageHandler))
	router.POST("/api/upload", makeAuthed(uploadHandler))
	router.Static("/files", "./files")

	//log.Fatal(autotls.Run(router, "entirety.praeceptamachinae.com", "walledgarden.praeceptamachinae.com"))
	router.Run("127.0.0.1:8091")

	/*	http.HandleFunc("/api/websocket", websocketHandler)
		log.Println("Listening for websockets on 0.0.0.0:82")
		log.Fatal(http.ListenAndServe("0.0.0.0:82", nil))
	*/
}

func storeMessage(id, message string) {
	//log.Printf("Writing message %v for user id: %v",message, id)
	b.Put("message", id, []byte(message))
}
func fetchMessage(id string) string {
	//log.Printf("Reading message for user id: %v", id)
	data, err := b.Get("message", id)
	if err == nil {
		return string(data)
	} else {
		return "none"
	}
}

func cameraUploaderHandler(c *gin.Context, id, token, top string) {
	displayPage(c, token, "files/js-webcam/1-basics.html")
}

func cameraWatcherHandler(c *gin.Context, id, token, top string) {
	displayPage(c, token, "files/watch.html")
}

func shutdownHandler(c *gin.Context, id, token, top string) {
	os.Exit(0)
}

func checkHandler(c *gin.Context, id, token, top string) {
	//log.Println("Fetching messages for id "+id)
	mess := fetchMessage(id)
	c.Writer.Write([]byte(string(mess)))
	//log.Println("Clearing messages for "+id)
	storeMessage(id, "none")
}

func imageHandler(c *gin.Context, id, token, top string) {
	mess := fetchMessage(id)
	log.Println("Fetching messages for id " + id)
	c.Writer.Write([]byte(string(mess)))
}

func uploadHandler(c *gin.Context, id, token, top string) {
	log.Println("upload handler")
	//parseErr := c.Request.ParseMultipartForm(32 << 20)
	//f parseErr != nil {
	//	log.Println("Failed to parse multipart")
	//	        return
	//	}
	//mimeData,_ := ioutil.ReadAll(c.Request.Body)
	c.Request.ParseMultipartForm(32 << 11)
	log.Println("parsed form")
	//infile, header, err := c.Request.FormFile("upimage")
	infile, _, _ := c.Request.FormFile("upimage")
	log.Println("Got file")

	//log.Printf("%v%+v%v", infile, header, err)
	data, _ := ioutil.ReadAll(infile)
	log.Println("Got data")
	//log.Println(string(data))
	storeMessage(id, string(data))
	log.Println("stored image for user ", id)
}

func tokenShowHandler(c *gin.Context, blah, token, top string) {
	sessionID := c.Query("id")
	displayPage(c, sessionID, "files/metro/1.html")
}

func displayLoginPage(c *gin.Context, id, token, top string) {
	displayPage(c, token, "files/loginSuccessful.html")
}

func aboutShowHandler(c *gin.Context, id, token, top string) {
	displayStaticPage(c, "files/metro/3.html")
}

func newTokenHandler(c *gin.Context, id, token, top string) {
	sessionToken := newToken(id)
	displayPage(c, sessionToken, "files/metro/1.html")
}
func messageHandler(c *gin.Context, id, prefix, top string) {
	message := c.Query("message")
	//c.Writer.Write([]byte("<p>message: " + message))
	sock, ok := sockets[id]
	ok = false
	if ok {
		sock <- message
	} else {
		storeMessage(id, message)
	}
	displaySubmenu(c, prefix)
}

func (s *server) Exists(bucket, key string) bool {

	var v []byte
	v = nil
	s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		v = b.Get([]byte(key))
		return nil
	})
	if v == nil {
		return false
	} else {
		return true
	}
	return false
}
func (s *server) Put(bucket, key string, val []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			panic(err)
		}
		b = tx.Bucket([]byte(bucket))
		if err = b.Put([]byte(key), val); err != nil {
			log.Printf("%v", err)
			panic(err)
		}
		//log.Printf("Wrote %v:%v to %v", key, string(val), bucket)
		return nil
	})
}

func (s *server) Get(bucket, key string) (data []byte, err error) {
	err = errors.New("Id '" + key + "' not found!")
	s.db.View(func(tx *bolt.Tx) error {
		bb := tx.Bucket([]byte(bucket))
		r := bb.Get([]byte(key))
		if r != nil && len(r) > 1 {
			data = make([]byte, len(r))
			copy(data, r)
			err = nil
		}
		return nil
	})
	return
}
func templateSet(template, before, after string) string {
	template = strings.Replace(template, before, after, -1)
	return template
}

func makeMessageMenu(template, number, id, message, description, extraMessage, prefix string) string {
	return makeMenu(template, number, id, "/api/send?id="+id+"&message="+message, description, extraMessage)
}

func makeMenu(template, number, id, message, description, extraMessage string) string {
	template = strings.Replace(template, "TARGET"+number, message, -1)
	template = strings.Replace(template, "DESCRIPTION"+number, description, -1)
	template = strings.Replace(template, "EXTRAMESSAGE"+number, extraMessage, -1)
	return template
}

func mainMenuHandler(c *gin.Context, id, token, top string) {
	displayNiceMainMenu(c, top)
}

func displayNiceMainMenu(c *gin.Context, prefix string) {
	page := GeneralMenu(prefix, [][]string{
		//[]string{"general/api/subMenu", "Remote Control", ""},
		//[]string{"quester/summary", "Unfinished Business", ""},
		//[]string{"general/api/cameraUploader", "Broadcast this camera", ""},
		//[]string{"general/api/cameraWatcher", "Watch another camera", ""},
		//[]string{"general/api/shutdown", "Shutdown the server", ""},
		//[]string{"ngfileserver/summary", "Files", ""},
                //[]string{"general/api/subMenu", "Remote Control", ""},
                //[]string{"quester/summary", "Unfinished Business", ""},
                []string{"quester/summary", "Unfinished Business", ""},
                //[]string{"general/api/cameraUploader", "Broadcast this camera", ""},
                //[]string{"general/api/cameraWatcher", "Watch another camera", ""},
                //[]string{"general/api/shutdown", "Shutdown the server", ""},
                []string{"entirety/local.html", "Maps", ""},
                //[]string{"ngfileserver/summary", "Files", ""},
                []string{"entiretymax/local.html", "Complete Maps", ""},
                []string{"entiretymax2/local.html", "Complete Maps(alternate)", ""},
                []string{"ngfileserver/summary", "Files", ""},
                []string{"fe/shop", "Shop Demo", ""},
	})
	c.Writer.Write([]byte(page))
}

func makeItem(prefix string, item []string) string {
	str := `<a href="` + prefix + item[0] + `"  class="card"><div>` + item[1] + `</div></a>`
	return str
}

func GeneralMenu(prefix string, items [][]string) string {
	templateb, _ := ioutil.ReadFile("files/generalMenu/index.html")
	template := string(templateb)
	var things []string
	for _, m := range items {
		things = append(things, makeItem(prefix, m))
	}
	menu := strings.Join(things, "\n")
	template = strings.Replace(template, "TITLE", "Main Menu", -1)
	template = strings.Replace(template, "MENU", menu, -1)
	return template
}

func displayStaticPage(c *gin.Context, filename string) {
	templateb, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Println(err)
	}
	template := string(templateb)
	log.Println(template)
	c.Writer.Write([]byte(template))
}

func displayPage(c *gin.Context, id, filename string) {
	templateb, _ := ioutil.ReadFile(filename)
	template := string(templateb)
	template = templateSet(template, "TOKEN", id)
	c.Writer.Write([]byte(template))
}

func subMenuHandler(c *gin.Context, id, prefix, top string) {
	displaySubmenu(c, prefix)
}

func displaySubmenu(c *gin.Context, prefix string) {
	/*
		template = makeMessageMenu(template, fmt.Sprintf("%v", 2), id, "build", "BUILD", "")
		template = makeMessageMenu(template, fmt.Sprintf("%v", 3), id, "run", "RUN", "")
		template = makeMenu(template, fmt.Sprintf("%v", 4), id, "/token?id="+id, "Access Token", fmt.Sprintf("%v", id))
		template = makeMenu(template, fmt.Sprintf("%v", 5), id, "/newToken"+id, "New Token", fmt.Sprintf("%v", id))
		template = makeMenu(template, "6", id, "api/mainMenu"+id, "Back to Menu", id)
	*/
	page := GeneralMenu(prefix, [][]string{
		[]string{"api/send?message=cancel", "Cancel", ""},
		[]string{"api/send?message=build", "Build", ""},
		[]string{"api/send?message=run", "Run", ""},
		[]string{"token", "Show token", ""},
		[]string{"newToken", "New token", ""},
		[]string{"api/mainMenu", "Main Menu", ""},
	})
	c.Writer.Write([]byte(page))
}

func isNewUser(id string) bool {
	if b.Exists("sessionIDs", id) {
		return true
	} else {
		return false
	}
}

func newToken(id string) string {
	sessionToken := fmt.Sprintf("%v", r.Int())
	b.Put("sessionTokens", sessionToken, []byte(id))
	b.Put("sessionIDs", id, []byte(sessionToken))
	return sessionToken
}

//Functions to deal with the websocket clients

//We just echo messages straight to the client
/*
var upgrader = websocket.Upgrader{} // use default options

func websocketHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Opening websocket for", r)
	sessionToken, ok := r.URL.Query()["id"]
	if !ok {
		log.Println("Invalid or missing id in query string!")
		return
	}
	id := sessionTokenToId(sessionToken[0])
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()
	var ch chan string
	ch = make(chan string)
	sockets[id] = ch
	defer close(ch)
	for {


		for mess := range sockets[id] {
			c.WriteMessage(websocket.TextMessage, []byte(mess))
		}
	}
}
*/
