package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/danilopolani/gocialite"
	"github.com/donomii/gin"
	"github.com/gin-gonic/autotls"
	uuid "github.com/satori/go.uuid"

	"github.com/gorilla/websocket"

	_ "github.com/philippgille/gokv"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/xlzd/gotp"
)

type userData_t struct {
	Id         string
	ForeignIDs []string
	Token      string
	QRcode     []byte
	OtpSecret  string
	UserName   string
}

var upGrader = websocket.Upgrader{

	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var gocial = gocialite.NewDispatcher()

// The url of the top of the authenticated part of your website
var baseUrl string = "https://entirety.praeceptamachinae.com/secure/"
var develop = false
var accessLog io.Writer
var db_prefix string = "authentigate_"
var providerSecrets map[string]map[string]string
var r *rand.Rand



// Turn errors into panics so we can catch them in the otp level handler and log them
func check(e error) {
	if e != nil {
		panic(e)
	}
}

// Create a log message in combined log format
func format_clf(c *gin.Context, id, responseCode, responseSize string) string {
	return fmt.Sprintf("%v - %v [%v] \"%v %v %v\" %v %v \"%v\" \"%v\"", c.ClientIP(), id, time.Now(), c.Request.Method, c.Request.RequestURI, c.Request.Proto, responseCode, responseSize, c.Request.Referer(), c.Request.UserAgent())
	//	127.0.0.1 - frank [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326 "http://www.example.com/start.html" "Mozilla/4.08 [en] (Win98; I ;Nav)"
}

func makeSecureZoneUrl(token string, siteUrl string) string {
	url := fmt.Sprintf("%v%v", token, siteUrl)
	//Strip double slashes
	url = strings.Replace(url, "//", "/", -1)
	return baseUrl + url
}

// Check that the revocable session token is valid, load the user details, and call the provided handler for the url
// Or, redirect them to the login page
func makeAuthedRelay(handlerFunc func(*gin.Context, string, string, *Redirect, bool), relay *Redirect, bans []string) func(c *gin.Context) {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Failure while processing %v: %v", c.Request.URL, r)
				log.Printf("Failure while processing %v: %v", c.Request.URL, r)
				debug.PrintStack()
				c.Status(500)
				displayPage(c, "", "files/BackendFailure.html", nil, nil)
			}
		}()

		url := c.Request.URL.String()
		for _, ban := range bans {
			if strings.Contains(url, ban) {
				c.Status(403)
				displayPage(c, "", "files/BackendFailure.html", nil, nil)
				return
			}
		}

		token, err := c.Cookie("AuthentigateSessionToken")
		useCookie := true
		if err == nil {
			log.Println("Found authentigate cookie: ", token)
		} else {
			log.Println("Cookie not found, using token")
			log.Println(err)
			token = c.Param("token")
			useCookie = false
		}
		id := sessionTokenToId(token)
		log.Printf("session: %v id: %v\n", token, id)
		if id == "" {
			log.Printf("Login failure for token: '%v'", token)
			frontPageHandler(c)
		} else {
			//if useCookie {
			//token = "c"
			//}
			handlerFunc(c, id, token, relay, useCookie)
		}
	}

}

type Redirect struct {
	From, To, Tipe, Name string
	CopyHeaders          []string
}
type Config struct {
	Redirects []Redirect
	Port      int
	BaseUrl   string
	HostNames []string
	Bans      []string
	LogFile   string
	Secure    bool
}

var config *Config

func main() {
	config = LoadConfig("config.json")
	if config == nil {
		fmt.Printf("Failed to read config file, exiting!\n")
		os.Exit(1)
	}
	if config.BaseUrl != "" {
		baseUrl = config.BaseUrl
	}
	var err error
	var f *os.File
	f, err = os.Create("accessLog")
	check(err)
	accessLog = bufio.NewWriterSize(f, 999999) //Golang!
	flag.BoolVar(&develop, "develop", false, "Allow log in with no password")
	flag.StringVar(&baseUrl, "base-url", baseUrl, "The top level url for the authenticated section of your site")
	flag.Parse()

	var shutdownFunc func()
	b, err, shutdownFunc = newAuthDB(db_prefix)
	check(err)
	defer shutdownFunc()

	if develop {
		baseUrl = "http://localhost:8000/secure/"
	}

	r = rand.New(rand.NewSource(time.Now().UnixNano()))
	router := gin.Default()

	router.GET("/", frontPageHandler)
	//User management pages
	router.GET("/manage/:token/token", makeAuthedRelay(tokenShowHandler, nil, config.Bans))
	router.GET("/manage/:token/updateUser", makeAuthedRelay(updateUserHandler, nil, config.Bans))
	router.POST("/manage/:token/updateUser", makeAuthedRelay(updateUserHandler, nil, config.Bans))
	router.GET("/manage/:token/newToken", makeAuthedRelay(newTokenHandler, nil, config.Bans))
	router.GET("/totplogin", makeAuthedRelay(totploginHandler, nil, config.Bans))
	router.POST("/totplogin", makeAuthedRelay(totploginHandler, nil, config.Bans))

	for _, loopPtr := range config.Redirects {
		relay := loopPtr
		fmt.Printf("Adding route from %v, to %v\n", relay.From, relay.To)
		switch relay.Tipe {
		case "GET":
			router.GET(relay.From, makeAuthedRelay(relayGetHandler, &relay, config.Bans))
		case "POST":
			router.POST(relay.From, makeAuthedRelay(relayPostHandler, &relay, config.Bans))
		case "PUT":
			router.PUT(relay.From, makeAuthedRelay(relayPutHandler, &relay, config.Bans))
		default:
			panic("Unsupported type for relay")
		}
	}

	//These are required to handle oauth2
	router.GET("/auth/:provider", redirectHandler)
	router.GET("/auth/:provider/callback", callbackHandler)

	//Drop CSS and js libraries in here
	router.Static("/files", "./files")
	router.Static("/qrcodes", "./qrcodes")
	router.Static("/favicon.ico", "./favicon.ico")
	if develop {
		router.GET("/develop/auth/callback", developCallbackHandler)
	}

	if develop {
		router.Run("127.0.0.1:8000")
	} else {
		if config.Secure {
			log.Fatal(autotls.Run(router, config.HostNames...))
		} else {
			router.Run(fmt.Sprintf("127.0.0.1:%v", config.Port))
		}
	}
}

func updateUserHandler(c *gin.Context, id string, token string, relay *Redirect, useCookie bool) {
	user := LoadUser(id)
	if user == nil {
		panic("User not found for sessionid:" + token + "!")
	}

	r := c.Request
	w := c.Writer
	if err := r.ParseForm(); err != nil {
		fmt.Fprintf(w, "ParseForm() err: %v", err)
		return
	}
	username := r.FormValue("username")
	if username == "" {
		displayPage(c, "", "files/showToken.html", nil, map[string]string{"UPDATEUSERURL": "/manage/" + token + "/updateUser"})
		return
	}
	//FIXME race condition here
	userExists := b.Exists("userNames", username)
	if userExists {
		displayPage(c, "", "files/showToken.html", nil, map[string]string{"UPDATEUSERURL": "/manage/" + token + "/updateUser", "ERROR": "Username already exists"})
		return
	}

	user.UserName = username
	if user.UserName != "" {
		user.QRcode = generateTOTPWithSecret(user.OtpSecret, user.UserName)
	}
	SaveUser(user)
	png := user.QRcode
	//Save the png to a temporary directory then server it
	tmpDir := "qrcodes"
	os.Mkdir(tmpDir, 0700)
	fname := fmt.Sprintf("%v/%v.png", tmpDir, token)
	ioutil.WriteFile(fname, png, 0600)
	b.UserNames.Set(username, user.Id)
	displayPage(c, token, "files/showToken.html", nil, map[string]string{"TOKEN": token, "QRCODE": "/" + fname, "UPDATEUSERURL": "/manage/" + token + "/updateUser", "USERNAME": username})
	return
}

func totploginHandler(c *gin.Context, id string, token string, relay *Redirect, useCookie bool) {
	r := c.Request
	w := c.Writer
	if err := r.ParseForm(); err != nil {
		fmt.Fprintf(w, "ParseForm() err: %v", err)
		return
	}
	username := r.FormValue("username")

	if username == "" {
		displayPage(c, "", "files/totplogin.html", nil, nil)
		return
	}

	code := r.FormValue("code")
	var userId string
	found, err := b.UserNames.Get(username, &userId)
	if err != nil {
		fmt.Fprintf(w, "Error: %v", err)
		return
	}
	if !found {
		fmt.Fprintf(w, "User not found")
		return
	}
	user := LoadUser(userId)
	if user == nil {
		fmt.Fprintf(w, "User not found")
		return
	}
	totp := gotp.NewDefaultTOTP(user.OtpSecret)
	ok := totp.Verify(code, time.Now().Unix())
	if !ok {
		fmt.Fprintf(w, "Invalid code")
		return
	}
	displayLoginSuccessfulPage(c, userId, user.Token)

}

// Show homepage with login URL
func frontPageHandler(c *gin.Context) {
	subs := map[string]string{}
	if develop {

		subs["DEVELOPER"] = "<hr/><a href='/develop/auth/callback'><button>Login with no password</button></a><br>"
	} else {
		subs["DEVELOPER"] = ""
	}
	displayPage(c, "", "files/frontpage.html", nil, subs)
}

// Show the user their revocable token
func tokenShowHandler(c *gin.Context, id string, token string, relay *Redirect, useCookie bool) {

	user := LoadUser(id)
	if user == nil {
		panic("User not found for sessionid:" + token + "!")
	}

	if user.UserName != "" {
		user.QRcode = generateTOTPWithSecret(user.OtpSecret, user.UserName)
		SaveUser(user)
	}
	png := user.QRcode
	//Save the png to a temporary directory then server it
	tmpDir := "qrcodes"
	os.Mkdir(tmpDir, 0700)
	fname := fmt.Sprintf("%v/%v.png", tmpDir, token)
	ioutil.WriteFile(fname, png, 0600)
	displayPage(c, token, "files/showToken.html", nil, map[string]string{"QRCODE": "/" + fname, "UPDATEUSERURL": "/manage/" + token + "/updateUser"})
}

// Show the user the successful login message
func displayLoginSuccessfulPage(c *gin.Context, id string, sessionToken string) {
	displayPage(c,
		//Add switch here for cookie/url token mode
		sessionToken,
		"files/loginSuccessful.html",
		map[string]string{"AuthentigateSessionToken": sessionToken},
		nil)
}

func setupNewUser(c *gin.Context, foreignID string, token string) string {

	u1, _ := uuid.NewV4()
	fmt.Printf("new user UUIDv4: %s\n", u1)
	user := userData_t{}
	user.Id = u1.String()
	user.ForeignIDs = []string{foreignID}
	SaveUser(&user)
	user.Token = newToken(user.Id) //Newtoken also saves user data

	b.ForeignIDs.Set(foreignID, user.Id)
	b.SessionTokens.Set(user.Token, user.Id)

	return user.Token
}

func newTokenHandler(c *gin.Context, id string, token string, relay *Redirect, useCookie bool) {
	sessionToken := newToken(id)
	displayPage(c, sessionToken, "files/showToken.html", nil, nil)
}

// Display a html file, inserting the revocable session token as needed
func displayPage(c *gin.Context, token, filename string, cookies map[string]string, subs map[string]string) {
	templateb, _ := ioutil.ReadFile(filename)
	template := string(templateb)
	template = templateSet(template, "TOKEN", token)
	template = templateSet(template, "BASE", baseUrl)
	template = templateSet(template, "SECUREURL", makeSecureZoneUrl(token, ""))
	for k, v := range subs {
		template = templateSet(template, k, v)
	}
	if cookies != nil {
		fmt.Printf("Setting cookies %v\n", cookies)
		for cookieName, cookieValue := range cookies {
			http.SetCookie(c.Writer, &http.Cookie{Name: cookieName, Value: cookieValue, Path: "/"})
		}
	}
	c.Writer.Write([]byte(template))
}

// Redirect to default microservice, using PUT
func relayPutHandler(c *gin.Context, id, token string, relay *Redirect, useCookie bool) {

	api := c.Param("api")
	log.Printf("PUT api : %v with id %v\n", api, id)
	bodyData, _ := ioutil.ReadAll(c.Request.Body)
	client := &http.Client{}
	uribits := strings.SplitN(c.Request.RequestURI, "?", 2)
	params := ""
	if len(uribits) > 1 {
		params = "?" + uribits[1]
	}

	req, err := http.NewRequest("PUT", relay.To+api+params, nil)

	AddAuthToRequest(req, id, token, baseUrl, relay, useCookie)
	forwarded_for := c.Request.Header.Get("X-Forwarded-For")
	req.Header.Add("X-Forwarded-For", forwarded_for)
	req.Header.Add("X-Forwarded-For", c.Request.RemoteAddr)
	req.Header.Add("X-Real-IP", c.RemoteIP())
	req.Header.Add("X-Forwarded-Port", fmt.Sprintf("%v", config.Port))
	req.Header.Add("X-Forwarded-Proto", c.Request.Proto)
	req.Header.Add("X-Forwarded-Host", c.Request.Host)

	//Copy the bare minimum needed for a post request
	//FIXME:  Move this into config file, allow configuration per-endpoint
	CopyHeaders := []string{"Content-Type", "Content-Length", "Content-Disposition"}
	for _, h := range CopyHeaders {
		req.Header.Add(h, c.Request.Header.Get(h))
	}

	CopyHeaders = relay.CopyHeaders
	for _, h := range CopyHeaders {
		req.Header.Add(h, c.Request.Header.Get(h))
	}

	req.Body = ioutil.NopCloser(bytes.NewReader(bodyData))

	log.Printf("PUT %v\n", req.URL)

	//Do it
	resp, err := client.Do(req)
	check(err)
	respData, err := ioutil.ReadAll(resp.Body)
	check(err)

	//Copy back the bare minimum needed
	c.Header("Content-Type", resp.Header.Get("Content-Type"))
	c.Header("Content-Length", resp.Header.Get("Content-Length"))
	c.Status(resp.StatusCode)
	//Write the result
	c.Writer.Write(respData)
	log.Printf("redirect PUT api %v, %v, %v\n", id, api, req.URL)
	accessLog.Write([]byte(format_clf(c, id, fmt.Sprintf("%v", resp.StatusCode), fmt.Sprintf("%v", resp.ContentLength)) + "\n"))
}

// Redirect to default microservice, using POST
func relayPostHandler(c *gin.Context, id, token string, relay *Redirect, useCookie bool) {

	api := c.Param("api")
	log.Printf("POST api : %v with id %v\n", api, id)
	fmt.Printf("POST api : %v with id %v\n", api, id)
	bodyData, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		panic(err)
	}
	client := &http.Client{}
	uribits := strings.SplitN(c.Request.RequestURI, "?", 2)
	params := ""
	if len(uribits) > 1 {
		params = "?" + uribits[1]
	}

	req, err := http.NewRequest("POST", relay.To+api+params, nil)

	AddAuthToRequest(req, id, token, baseUrl, relay, useCookie)


	CopyHeaders := relay.CopyHeaders
	log.Printf("Forwarding headers %v\n", CopyHeaders)
	log.Printf("Request %+v\n", c)
	log.Printf("Request body %v\n", string(bodyData))
	for k, _ := range c.Request.Header {
		req.Header.Add(k, c.Request.Header.Get(k))
		log.Printf("Copyheader: %v, %v\n", k, c.Request.Header.Get(k))
	}

	forwarded_for := c.Request.Header.Get("X-Forwarded-For")
	req.Header.Add("X-Forwarded-For", forwarded_for)
	req.Header.Add("X-Forwarded-For", c.Request.RemoteAddr)
	req.Header.Add("X-Real-IP", c.RemoteIP())
	req.Header.Add("X-Forwarded-Port", fmt.Sprintf("%v", config.Port))
	req.Header.Add("X-Forwarded-Proto", c.Request.Proto)
	req.Header.Add("X-Forwarded-Host", c.Request.Host)

	log.Printf("Sending Request %+v\n", req)
	req.Body = ioutil.NopCloser(bytes.NewReader(bodyData))

	log.Printf("POST %v\n", req.URL)

	//Do it
	resp, err := client.Do(req)
	var respData []byte
	if resp != nil {
		if resp.Body != nil {
			respData, err = ioutil.ReadAll(resp.Body)
			check(err)
		}

		for k, h := range resp.Header {
			c.Header(k, h[0])
		}

		//Write the result
		c.Writer.Write(respData)
		log.Printf("redirect POST api %v, %v, %v\n", id, api, req.URL)
		accessLog.Write([]byte(format_clf(
			c,
			id,
			fmt.Sprintf("%v", resp.StatusCode),
			fmt.Sprintf("%v", resp.ContentLength)) + "\n"))
	} else {
		log.Printf("redirect POST api %v, %v, %v\n", id, api, req.URL)
		log.Printf("redirect failed %+V\n", resp)

	}
}

// Not very functional, but it will do for now
func AddAuthToRequest(req *http.Request, id, token, baseUrl string, relay *Redirect, useCookie bool) {
	microserviceBaseUrl := fmt.Sprintf("%vc/%v/", baseUrl, relay.Name)
	microserviceTokenUrl := fmt.Sprintf("%v%v/%v/", baseUrl, token, relay.Name)
	siteTopUrl := fmt.Sprintf("%v%v/", baseUrl, token)
	req.Header.Add("authentigate-id", id)
	req.Header.Add("authentigate-token", token)
	//req.Header.Add("X-Auth-Token", token)
	req.Header.Add("authentigate-base-url", microserviceBaseUrl)
	req.Header.Add("authentigate-base-token-url", microserviceTokenUrl)
	req.Header.Add("authentigate-top-url", siteTopUrl)
}

// Redirect to default microservice, using GET
func relayGetHandler(c *gin.Context, id, token string, relay *Redirect, useCookie bool) {

	api := c.Param("api")

	client := &http.Client{}

	//Pass params through untouched (security implications?)
	uribits := strings.SplitN(c.Request.RequestURI, "?", 2)
	params := ""
	if len(uribits) > 1 {
		params = "?" + uribits[1]
	}

	//TODO make this configurable from a file
	req, err := http.NewRequest("GET", relay.To+api+params, nil)

	AddAuthToRequest(req, id, token, baseUrl, relay, useCookie)

	forwarded_for := c.Request.Header.Get("X-Forwarded-For")
	req.Header.Add("X-Forwarded-For", forwarded_for)
	req.Header.Add("X-Forwarded-For", c.Request.RemoteAddr)
	req.Header.Add("X-Real-IP", c.RemoteIP())
	req.Header.Add("X-Forwarded-Port", fmt.Sprintf("%v", config.Port))
	req.Header.Add("X-Forwarded-Proto", c.Request.Proto)
	req.Header.Add("X-Forwarded-Host", c.Request.Host)
	CopyHeaders := relay.CopyHeaders
	for _, h := range CopyHeaders {
		req.Header.Add(h, c.Request.Header.Get(h))
	}

	upgradeH := c.Request.Header.Get("upgrade")
	if upgradeH == "websocket" {
		log.Printf("upgrade GET api %v, %v, %v\n", id, api, req.URL)
		upgradeAndHandle(c, req)
		return
	}

	log.Printf("redirect GET api %v, %v, %v\n", id, api, req.URL)
	resp, err := client.Do(req)
	check(err)
	respData, err := ioutil.ReadAll(resp.Body)

	c.Header("Content-Type", resp.Header.Get("Content-Type"))
	c.Header("Content-Disposition", resp.Header.Get("Content-Disposition"))
	c.Status(resp.StatusCode)
	c.Writer.Write(respData)
	logMess := []byte(format_clf(c, id, fmt.Sprintf("%v", resp.StatusCode), fmt.Sprintf("%v", resp.ContentLength)) + "\n")
	accessLog.Write(logMess)
}

// Redirect to correct oAuth URL
func redirectHandler(c *gin.Context) {
	// Retrieve provider from route
	provider := c.Param("provider")

	// In this case we use a map to store our secrets
	if providerSecrets == nil {
		providerSecrets = map[string]map[string]string{}
		configData, err := ioutil.ReadFile("provider_secrets.json")
		if err != nil {
			panic(fmt.Sprintf("Could not read provider secrets from file provider_secrets.json: %v", err))
		}

		err = json.Unmarshal(configData, &providerSecrets)
		if err != nil {
			panic(fmt.Sprintf("Failed to parse provider secrets file provider_secrets.json: %v", err))
		}
	}

	providerScopes := map[string][]string{
		"github":    []string{"public_repo"},
		"linkedin":  []string{},
		"facebook":  []string{},
		"google":    []string{},
		"bitbucket": []string{},
		"amazon":    []string{},
		"slack":     []string{},
	}

	providerData := providerSecrets[provider]
	actualScopes := providerScopes[provider]

	authURL, err := gocial.New().
		Driver(provider).
		Scopes(actualScopes).
		Redirect(
			providerData["clientID"],
			providerData["clientSecret"],
			providerData["redirectURL"],
		)

	// Check for errors (usually driver not valid)
	if err != nil {
		c.Writer.Write([]byte("Error: " + err.Error()))
		return
	}

	// Redirect with authURL
	c.Redirect(http.StatusFound, authURL)
}

// Quick and dirty HTML templating
func templateSet(template, before, after string) string {
	template = strings.Replace(template, before, after, -1)
	return template
}

// Does the user exist in our user database?
func isNewUser(id string) bool {
	var user userData_t
	if found, _ := b.Users.Get(id, &user); found {
		return true
	} else {
		return false
	}
}

func testOTPVerify(secret string) {
	totp := gotp.NewDefaultTOTP(secret)
	otpValue := totp.Now()
	fmt.Println("current one-time password is:", otpValue)

	ok := totp.Verify(otpValue, time.Now().Unix())
	fmt.Println("verify OTP success:", ok)
}

func generateTOTPWithSecret(secret, username string) []byte {
	if username == "" {
		panic("Username must be set")
	}
	totp := gotp.NewDefaultTOTP(secret)
	fmt.Println("current one-time password is:", totp.Now())

	uri := totp.ProvisioningUri(username, "Authentigate")
	fmt.Println(uri)
	q, err := qrcode.New(uri, qrcode.High)
	if err != nil {
		panic(err)
	}

	p, err := q.PNG(256)
	if err != nil {
		panic(err)
	}
	return p
}

//Authentigate provides revocable tokens for users.  Tokens are mapped to user IDs by authentigate
//
//Generate a new revocable token

func newToken(id string) string {
	sessionToken := fmt.Sprintf("%v", r.Int())
	log.Printf("Setting id:token %v:%v\n", id, sessionToken)
	b.Put("sessionTokens", sessionToken, []byte(id))

	user := LoadUser(id)

	if user == nil {
		panic("User not found")
	}

	user.Token = sessionToken

	user.OtpSecret = gotp.RandomSecret(16)
	if user.UserName != "" {
		user.QRcode = generateTOTPWithSecret(user.OtpSecret, user.UserName)
	}
	SaveUser(user)
	return sessionToken
}

// Handle callback of providers - google, facebook etc
func callbackHandler(c *gin.Context) {
	// Retrieve query params for state and code
	state := c.Query("state")
	code := c.Query("code")
	provider := c.Param("provider")

	// Handle callback and check for errors
	foreignUser, _, err := gocial.Handle(state, code)
	if err != nil {
		c.Writer.Write([]byte("Error: " + err.Error()))
		return
	}

	log.Printf("User: %#v\n\n", foreignUser)
	log.Printf("User ID: %#v\n\n", foreignUser.ID)

	foreignID := provider + foreignUser.ID
	id := foreignIdToId(foreignID)
	var token string
	if id == "" {
		//First time user.  Create an account for them, because they have already gone through the authentication process using OAuth2
		token = setupNewUser(c, foreignID, "")
	} else {
		user := LoadUser(id)
		token = user.Token
	}

	displayLoginSuccessfulPage(c, id, token)

}

// Handle callback of providers - google, facebook etc
func developCallbackHandler(c *gin.Context) {
	if develop {

		id := "1"
		var token string
		if !b.Exists("users", id) {
			fmt.Printf("Creating new user with id 1\n")
			token = setupNewUser(c, id, "")
		} else {

			token = idToSessionToken(id)
			fmt.Printf("Found user with id 1, token %v\n", token)
		}
		if token == "" {
			token = setupNewUser(c, id, "")
		}

		displayLoginSuccessfulPage(c, id, token)
	} else {
		panic("Not allowed!")
	}
}

//Custom fileserver relay
//
//Eventually, combine this with the rest of the code using configuration files

func ngfileserverRelayParameterisedHandler(c *gin.Context, id, token string, relay *Redirect, useCookie bool) {
	c.Header("Cache-Control", "no-cache,no-store")
	api := c.Param("api")
	log.Printf("api call: %v with id %v\n", api, id)

	client := &http.Client{}
	uribits := strings.SplitN(c.Request.RequestURI, "?", 2)
	params := ""
	if len(uribits) > 1 {
		params = "?" + uribits[1]
	}
	req, err := http.NewRequest("GET", relay.To+api+params, nil)

	AddAuthToRequest(req, id, token, baseUrl, relay, useCookie)
	log.Printf("Relaying call to %v\n", req.URL)
	resp, err := client.Do(req)
	respData, err := ioutil.ReadAll(resp.Body)
	check(err)
	c.Header("Content-Type", resp.Header.Get("Content-Type"))
	c.Status(resp.StatusCode)
	c.Writer.Write(respData)
	accessLog.Write([]byte(format_clf(c, id, fmt.Sprintf("%v", resp.StatusCode), fmt.Sprintf("%v", resp.ContentLength)) + "\n"))
}

func ngfileserverRelayHandler(c *gin.Context, id, token, target string) {

	api := c.Param("api")
	log.Printf("api call: %v with id %v\n", api, id)

	completeBaseUrl := fmt.Sprintf("%v%v/ngfileserver/", baseUrl, token)

	client := &http.Client{}

	req, err := http.NewRequest("GET", target+api, nil)

	req.Header.Add("authentigate-id", id)
	req.Header.Add("authentigate-token", token)
	req.Header.Add("authentigate-base-url", completeBaseUrl)
	log.Printf("Relaying call to %v\n", req.URL)
	resp, err := client.Do(req)
	respData, err := ioutil.ReadAll(resp.Body)
	check(err)
	c.Writer.Write(respData)
	accessLog.Write([]byte(format_clf(c, id, fmt.Sprintf("%v", resp.StatusCode), fmt.Sprintf("%v", resp.ContentLength)) + "\n"))
}

func ngfileserverPutRelayHandler(c *gin.Context, id, token, target string) {

	api := c.Param("api")
	log.Printf("api call: %v with id %v\n", api, id)

	completeBaseUrl := fmt.Sprintf("%v%v/ngfileserver/", baseUrl, token)

	client := &http.Client{}

	req, err := http.NewRequest("PUT", target+api, nil)

	req.Header.Add("authentigate-id", id)
	req.Header.Add("authentigate-token", token)
	req.Header.Add("authentigate-base-url", completeBaseUrl)
	//FIXME: Move to config file, allow configuration per-endpoint
	CopyHeaders := []string{"Content-Type", "Content-Length"}
	for _, h := range CopyHeaders {
		req.Header.Add(h, c.Request.Header.Get(h))
	}
	bodyData, _ := ioutil.ReadAll(c.Request.Body)
	req.Body = ioutil.NopCloser(bytes.NewReader(bodyData))
	log.Printf("Relaying put call to %v\n", req.URL)
	resp, err := client.Do(req)
	respData, err := ioutil.ReadAll(resp.Body)
	check(err)
	c.Header("Content-Type", resp.Header.Get("Content-Type"))
	c.Status(resp.StatusCode)
	c.Writer.Write(respData)
	log.Printf("redirect PUT api %v, %v, %v\n", id, api, req.URL)
	accessLog.Write([]byte(format_clf(c, id, fmt.Sprintf("%v", resp.StatusCode), fmt.Sprintf("%v", resp.ContentLength)) + "\n"))
}
