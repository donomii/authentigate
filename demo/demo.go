package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	//"github.com/gin-gonic/autotls"

	"github.com/gin-gonic/gin"
)

var safe bool = false

type Basket struct {
}

type User struct {
	Name        string
	Basket      Basket
	UsedCoupons []string
	Orders      []Basket
}

func LoadJson(id string) *User {
	var out *User
	res, err := ioutil.ReadFile(fmt.Sprintf("userdata/%v.json", id))
	err = json.Unmarshal(res, &out)
	if err != nil {
		log.Println("Could not load user", err)
		//panic(err)
	}
	if out == nil {
		t := User{Name: id}
		out = &t
	}
	return out
}

func SaveJson(id string, tasks *User) {
	payload, err := json.Marshal(tasks)
	if err != nil {
		panic("Could not marshall quests")
	}
	ioutil.WriteFile(fmt.Sprintf("userdata/%v.json", id), payload, 0600)
}

func makeAuthed(handlerFunc func(*gin.Context, string, string)) func(c *gin.Context) {
	return func(c *gin.Context) {
		id := c.Request.Header.Get("authentigate-id")
		baseUrl := c.Request.Header.Get("authentigate-base-url")
		log.Printf("Got real user id: '%v'", id)
		handlerFunc(c, id, baseUrl)
	}

}

func summary(c *gin.Context, id string, token string) {
	basket, err := json.Marshal(LoadJson(id).Basket)
	if err != nil {
		panic(err)
	}
	c.Writer.Write(basket)
}

func detailed(c *gin.Context, id string, token string) {
	q := c.Query("q")
	c.Writer.Write([]byte(`
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>jsTree test</title>
  <!-- 2 load the theme CSS file --><link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/jstree/3.2.1/themes/default/style.min.css" />
  <!-- 4 include the jQuery library -->
  <script src="https://cdnjs.cloudflare.com/ajax/libs/jquery/1.12.1/jquery.min.js"></script>
  <!-- 5 include the minified jstree source -->
  <script src="https://cdnjs.cloudflare.com/ajax/libs/jstree/3.2.1/jstree.min.js"></script>
<script>
window.addEventListener( "pageshow", function ( event ) {
  var historyTraversal = event.persisted || 
                         ( typeof window.performance != "undefined" && 
                              window.performance.navigation.type === 2 );
  if ( historyTraversal ) {
    // Handle page restore.
    window.location.reload();
  }
});
</script>
</head>
<body>
   ` + taskDisplay(id, q, true) + `
</body>
</html>
`))
}

func forceTrailingSlash(path string) string {
	if strings.HasSuffix(path, "/") {
		return path
	} else {
		return path + "/"
	}
}

func toggle(c *gin.Context, id string, token string) {
	upath := c.Query("path")
	fmt.Println("Toggling", upath)

	topNode := LoadJson(id)
	t := FindTask(upath, topNode)
	t.Checked = !t.Checked
	SaveJson(id, topNode)

}

func main() {
	os.Mkdir("userdata", 0700)
	router := gin.Default()
	serveQuester(router, "/shoppr/")
	router.Run("127.0.0.1:98")
}

func serveQuester(router *gin.Engine, prefix string) {

	router.GET(prefix+"summary", makeAuthed(summary))
	router.GET(prefix+"detailed", makeAuthed(detailed))
	router.POST(prefix+"addItem", makeAuthed(addItem))

	router.GET(prefix+"toggle", makeAuthed(toggle))
}

//Force nocache
var epoch = time.Unix(0, 0).Format(time.RFC1123)

var noCacheHeaders = map[string]string{
	"Expires":         epoch,
	"Cache-Control":   "no-cache, private, max-age=0",
	"Pragma":          "no-cache",
	"X-Accel-Expires": "0",
}

var etagHeaders = []string{
	"ETag",
	"If-Modified-Since",
	"If-Match",
	"If-None-Match",
	"If-Range",
	"If-Unmodified-Since",
}

func NoCache(f func(w http.ResponseWriter, r *http.Request)) func(w http.ResponseWriter, r *http.Request) {
	fn := func(w http.ResponseWriter, r *http.Request) {
		// Delete any ETag headers that may have been set
		for _, v := range etagHeaders {
			if r.Header.Get(v) != "" {
				r.Header.Del(v)
			}
		}

		// Set our NoCache headers
		for k, v := range noCacheHeaders {
			w.Header().Set(k, v)
		}

		f(w, r)
	}

	return fn
}
