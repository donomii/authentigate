package main

import (
	"errors"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
	"github.com/danilopolani/gocialite"
)

type Context struct {
	Writer  http.ResponseWriter
	Request *http.Request
}

func (c *Context) Query(key string) string {
	return c.Request.URL.Query().Get(key)
}

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

func makeAuthed(handlerFunc func(*Context, string, string, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := &Context{Writer: w, Request: r}
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
	mux := http.NewServeMux()

	mux.HandleFunc("GET /token", makeAuthed(tokenShowHandler))
	mux.HandleFunc("GET /about", makeAuthed(aboutShowHandler))
	mux.HandleFunc("GET /api/cameraUploader", makeAuthed(cameraUploaderHandler))
	mux.HandleFunc("GET /api/cameraWatcher", makeAuthed(cameraWatcherHandler))
	mux.HandleFunc("GET /api/shutdown", makeAuthed(shutdownHandler))
	mux.HandleFunc("GET /api/mainMenu", makeAuthed(mainMenuHandler))
	mux.HandleFunc("GET /api/subMenu", makeAuthed(subMenuHandler))
	mux.HandleFunc("GET /api/newToken", makeAuthed(newTokenHandler))
	mux.HandleFunc("GET /api/send", makeAuthed(messageHandler))
	mux.HandleFunc("GET /api/check", makeAuthed(checkHandler))
	mux.HandleFunc("GET /api/image", makeAuthed(imageHandler))
	mux.HandleFunc("POST /api/upload", makeAuthed(uploadHandler))

	mux.Handle("/files/", http.StripPrefix("/files/", http.FileServer(http.Dir("./files"))))

	fmt.Println("Starting menu module on 127.0.0.1:8091...")
	log.Fatal(http.ListenAndServe("127.0.0.1:8091", mux))
}

func storeMessage(id, message string) error {
	//log.Printf("Writing message %v for user id: %v",message, id)
	return b.Put("message", id, []byte(message))
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

func cameraUploaderHandler(c *Context, id, token, top string) {
	displayPage(c, token, "files/js-webcam/1-basics.html")
}

func cameraWatcherHandler(c *Context, id, token, top string) {
	displayPage(c, token, "files/watch.html")
}

func shutdownHandler(c *Context, id, token, top string) {
	http.Error(c.Writer, fmt.Sprintf("Remote server shutdown is disabled"), http.StatusForbidden)
}

func checkHandler(c *Context, id, token, top string) {
	//log.Println("Fetching messages for id "+id)
	mess := []byte("none")
	err := b.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("message"))
		if bucket == nil {
			return nil
		}
		if value := bucket.Get([]byte(id)); value != nil {
			mess = append([]byte{}, value...)
		}
		return bucket.Delete([]byte(id))
	})
	if err != nil {
		http.Error(c.Writer, fmt.Sprintf("Could not read message for user %s: %v", id, err), http.StatusInternalServerError)
		return
	}
	c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
	menuData(c, http.StatusOK, "text/plain; charset=utf-8", mess)
	//log.Println("Clearing messages for "+id)
}

func imageHandler(c *Context, id, token, top string) {
	mess := fetchMessage(id)
	log.Println("Fetching messages for id " + id)
	contentType := http.DetectContentType([]byte(mess))
	if !supportedImage(contentType) {
		http.Error(c.Writer, fmt.Sprintf("Stored message for user %s is not a supported image", id), http.StatusUnsupportedMediaType)
		return
	}
	c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
	menuData(c, http.StatusOK, contentType, []byte(mess))
}

func uploadHandler(c *Context, id, token, top string) {
	//parseErr := c.Request.ParseMultipartForm(32 << 20)
	//f parseErr != nil {
	//	log.Println("Failed to parse multipart")
	//	        return
	//	}
	//mimeData,_ := ioutil.ReadAll(c.Request.Body)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<20)
	if err := c.Request.ParseMultipartForm(32 << 11); err != nil {
		status := http.StatusBadRequest
		var limitError *http.MaxBytesError
		if errors.As(err, &limitError) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(c.Writer, fmt.Sprintf("Could not parse image upload: %v", err), status)
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	//infile, header, err := c.Request.FormFile("upimage")
	infile, _, err := c.Request.FormFile("upimage")
	if err != nil {
		http.Error(c.Writer, fmt.Sprintf("Could not read upimage upload: %v", err), http.StatusBadRequest)
		return
	}
	defer infile.Close()

	//log.Printf("%v%+v%v", infile, header, err)
	data, err := io.ReadAll(infile)
	if err != nil {
		http.Error(c.Writer, fmt.Sprintf("Could not read uploaded image: %v", err), http.StatusBadRequest)
		return
	}
	//log.Println(string(data))
	if !supportedImage(http.DetectContentType(data)) {
		http.Error(c.Writer, fmt.Sprintf("Uploaded image must be PNG, JPEG, GIF, or WebP"), http.StatusUnsupportedMediaType)
		return
	}
	if err := b.Put("message", id, data); err != nil {
		http.Error(c.Writer, fmt.Sprintf("Could not store image for user %s: %v", id, err), http.StatusInternalServerError)
	}
}

func tokenShowHandler(c *Context, blah, token, top string) {
	managementToken := c.Request.Header.Get("authentigate-token")
	if managementToken == "" {
		http.Error(c.Writer, fmt.Sprintf("No authenticated token was supplied for token management"), http.StatusUnauthorized)
		return
	}
	http.Redirect(c.Writer, c.Request, "/manage/"+url.PathEscape(managementToken)+"/token", http.StatusSeeOther)
}

func displayLoginPage(c *Context, id, token, top string) {
	displayPage(c, token, "files/loginSuccessful.html")
}

func aboutShowHandler(c *Context, id, token, top string) {
	displayStaticPage(c, "files/metro/3.html")
}

func newTokenHandler(c *Context, id, token, top string) {
	managementToken := c.Request.Header.Get("authentigate-token")
	if managementToken == "" {
		http.Error(c.Writer, fmt.Sprintf("No authenticated token was supplied for token management"), http.StatusUnauthorized)
		return
	}
	http.Redirect(c.Writer, c.Request, "/manage/"+url.PathEscape(managementToken)+"/newToken", http.StatusSeeOther)
}
func messageHandler(c *Context, id, prefix, top string) {
	message := c.Query("message")
	//c.Writer.Write([]byte("<p>message: " + message))
	sock, ok := sockets[id]
	ok = false
	if ok {
		sock <- message
	} else {
		if err := storeMessage(id, message); err != nil {
			http.Error(c.Writer, fmt.Sprintf("Could not store message for user %s: %v", id, err), http.StatusInternalServerError)
			return
		}
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
}
func (s *server) Put(bucket, key string, val []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return err
		}
		b = tx.Bucket([]byte(bucket))
		if err = b.Put([]byte(key), val); err != nil {
			return err
		}
		//log.Printf("Wrote %v:%v to %v", key, string(val), bucket)
		return nil
	})
}

func (s *server) Get(bucket, key string) (data []byte, err error) {
	err = s.db.View(func(tx *bolt.Tx) error {
		bb := tx.Bucket([]byte(bucket))
		if bb == nil {
			return errors.New("Id '" + key + "' not found in bucket '" + bucket + "'!")
		}
		r := bb.Get([]byte(key))
		if r != nil {
			data = make([]byte, len(r))
			copy(data, r)
		} else {
			return errors.New("Id '" + key + "' not found in bucket '" + bucket + "'!")
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

func mainMenuHandler(c *Context, id, token, top string) {
	displayNiceMainMenu(c, top)
}

func displayNiceMainMenu(c *Context, prefix string) {
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
		[]string{"presence/room", "Presence", ""},
		[]string{"cameraview/", "Cameras", ""},
	})
	c.Writer.Write([]byte(page))
}

func makeItem(prefix string, item []string) string {
	str := `<a href="` + html.EscapeString(prefix+item[0]) + `"  class="card"><div>` + html.EscapeString(item[1]) + `</div></a>`
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

func displayStaticPage(c *Context, filename string) {
	templateb, err := os.ReadFile(filename)
	if err != nil {
		log.Println(err)
	}
	template := string(templateb)
	log.Println(template)
	c.Writer.Write([]byte(template))
}

func displayPage(c *Context, id, filename string) {
	templateb, _ := os.ReadFile(filename)
	template := string(templateb)
	template = templateSet(template, "TOKEN", html.EscapeString(id))
	c.Writer.Write([]byte(template))
}

func subMenuHandler(c *Context, id, prefix, top string) {
	displaySubmenu(c, prefix)
}

func displaySubmenu(c *Context, prefix string) {
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
		[]string{"api/newToken", "New token", ""},
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

func supportedImage(contentType string) bool {
	return contentType == "image/png" || contentType == "image/jpeg" || contentType == "image/gif" || contentType == "image/webp"
}


func menuData(c *Context, status int, contentType string, data []byte) {
	c.Writer.Header().Set("Content-Type", contentType)
	c.Writer.WriteHeader(status)
	c.Writer.Write(data)
}
