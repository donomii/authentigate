package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/donomii/goof"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	//"github.com/gin-gonic/autotls"

	"github.com/gin-gonic/gin"
	ginprometheus "github.com/zsais/go-gin-prometheus"
)

var safe bool = false

type UserData struct {
	LastTime          time.Time
	ExternalIPaddress string
	LocalIPaddress    string
}

type UserMap map[string]UserData

type Room struct {
	Users UserMap
}

var Rooms map[int]Room
var room_lock sync.Mutex

func makeAuthed(handlerFunc func(*gin.Context, string, string)) func(c *gin.Context) {
	return func(c *gin.Context) {
		id := c.Request.Header.Get("authentigate-id")
		baseUrl := c.Request.Header.Get("authentigate-base-url")
		if id == "" {
			id = "personalusermode"
		}
		log.Printf("Got real user id: '%v'", id)
		handlerFunc(c, id, baseUrl)
	}

}

//Make a sorted list of the keys in the map
func (m UserMap) Keys() []string {

	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	//Sort the keys
	sort.Strings(keys)
	return keys
}

func render_users(Users UserMap) string {
	userHtml := "<div id=\"users\">"
	ks := Users.Keys()
	for _, k := range ks {
		v := Users[k]
		diff := time.Now().Sub(v.LastTime)
		//if diff < 60 {
		levelg := goof.Clamp(255-int(diff.Seconds())*4, 0, 255)
		levelr := goof.Clamp(int(diff.Seconds())*4, 0, 255)
		userHtml = userHtml + fmt.Sprintf("<div><span class='box' style='background-color: #%02x%02x%02x;'>U</span>user %v: %v seconds(<a href=\"http://%v\">%v</a>,<a href=\"http://%v\">%v</a>)</div>", levelr, levelg, 1, k, int(diff.Seconds()), v.ExternalIPaddress, v.ExternalIPaddress, v.LocalIPaddress, v.LocalIPaddress)
		//}
	}
	userHtml = userHtml + "</div>"
	return userHtml

}

func handle_users(c *gin.Context, id string, token string) {
	room_lock.Lock()
	defer room_lock.Unlock()
	room_id, _ := strconv.Atoi(c.Query("id"))

	if Rooms == nil {
		Rooms = map[int]Room{}
	}
	room, ok := Rooms[room_id]
	if !ok {
		Rooms[room_id] = Room{Users: UserMap{}}
		handle_users(c, id, token)
		return
	}

	user_id := c.Query("host")
	if user_id != "" {
		id = user_id
	} else {
		user_id = c.Query("user")
		if user_id != "" {
			id = user_id
		}
	}
	userdata, ok := room.Users[id]
	if !ok {
		userdata = UserData{}

	}
	userdata.LastTime = time.Now()
	possibleIPs := c.Request.Header["X-Tinyproxy"]
	if len(possibleIPs) > 0 {
		userdata.ExternalIPaddress = possibleIPs[0]
	}
	//userdata.ExternalIPaddress = c.Request.Header["X-Forwarded-For"][0]
	userdata.LocalIPaddress = c.Query("localip")

	room.Users[id] = userdata

	c.Writer.Write([]byte(render_users(room.Users)))
}

func handle_room(c *gin.Context, id string, token string) {
	room_lock.Lock()
	defer room_lock.Unlock()
	room_id, _ := strconv.Atoi(c.Query("id"))

	if Rooms == nil {
		Rooms = map[int]Room{}
	}
	room, ok := Rooms[room_id]
	if !ok {
		Rooms[room_id] = Room{Users: UserMap{}}
		handle_room(c, id, token)
		return
	}

	user_id := c.Query("host")
	if user_id != "" {
		id = user_id
	} else {
		user_id = c.Query("user")
		if user_id != "" {
			id = user_id
		}
	}
	userdata, ok := room.Users[id]
	if !ok {
		userdata = UserData{}
		room.Users[id] = userdata
	}
	userdata.LastTime = time.Now()
	possibleIPs := c.Request.Header["X-Tinyproxy"]
	if len(possibleIPs) > 0 {
		userdata.ExternalIPaddress = possibleIPs[0]
	}
	userdata.LocalIPaddress = ""

	userHtml := render_users(room.Users)

	c.Writer.Write([]byte(`
<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>jsTree test</title>
<style>
.box {
  height: 20px;
  width: 20px;
  margin-bottom: 15px;
  border: 1px solid black;
  
background-color: green;
}
</style>


<script>
var ipaddress ="no ipaddress found";
// NOTE: window.RTCPeerConnection is "not a constructor" in FF22/23
var RTCPeerConnection = /*window.RTCPeerConnection ||*/ window.webkitRTCPeerConnection || window.mozRTCPeerConnection;

if (RTCPeerConnection) (function () {
    var rtc = new RTCPeerConnection({iceServers:[]});
    if (1 || window.mozRTCPeerConnection) {      // FF [and now Chrome!] needs a channel/stream to proceed
        rtc.createDataChannel('', {reliable:false});
    };
    
    rtc.onicecandidate = function (evt) {
        // convert the candidate to SDP so we can run it through our general parser
        // see https://twitter.com/lancestout/status/525796175425720320 for details
        if (evt.candidate) grepSDP("a="+evt.candidate.candidate);
    };
    rtc.createOffer(function (offerDesc) {
        grepSDP(offerDesc.sdp);
        rtc.setLocalDescription(offerDesc);
    }, function (e) { console.warn("offer failed", e); });
    
    
    var addrs = Object.create(null);
    addrs["0.0.0.0"] = false;
    function updateDisplay(newAddr) {
		ipaddress=newAddr;
    }
    
    function grepSDP(sdp) {
        var hosts = [];
        sdp.split('\r\n').forEach(function (line) { // c.f. http://tools.ietf.org/html/rfc4566#page-39
            if (~line.indexOf("a=candidate")) {     // http://tools.ietf.org/html/rfc4566#section-5.13
                var parts = line.split(' '),        // http://tools.ietf.org/html/rfc5245#section-15.1
                    addr = parts[4],
                    type = parts[7];
                if (type === 'host') updateDisplay(addr);
            } else if (~line.indexOf("c=")) {       // http://tools.ietf.org/html/rfc4566#section-5.7
                var parts = line.split(' '),
                    addr = parts[2];
                updateDisplay(addr);
            }
        });
    }
})(); else {
    document.getElementById('list').innerHTML = "<code>ifconfig | grep inet | grep -v inet6 | cut -d\" \" -f2 | tail -n1</code>";
    document.getElementById('list').nextSibling.textContent = "In Chrome and Firefox your IP should display automatically, by the power of WebRTCskull.";
}

</script>



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

function updateUsers() {
	var token = "` + token + `";
	$.get(token+"users?localip="+ipaddress+"&host="+window.location.host, function(data) {
	     $("#users").replaceWith(data);
	});
	
	setTimeout(updateUsers, 3000);
}
updateUsers();
</script>
</head>
<body>
   ` + userHtml + `
</body>
</html>
`))
}

func main() {
	os.Mkdir("quester", 0700)
	router := gin.Default()
	p := ginprometheus.NewPrometheus("gin")
	p.Use(router)

	serveQuester(router, "/presence/")
	http.Handle("/metrics", promhttp.Handler())
	router.Run("127.0.0.1:8093")
}

func serveQuester(router *gin.Engine, prefix string) {

	router.GET(prefix+"room", makeAuthed(handle_room))
	router.GET(prefix+"users", makeAuthed(handle_users))
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
