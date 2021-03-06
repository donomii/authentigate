package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
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

	"github.com/boltdb/bolt"
	"github.com/danilopolani/gocialite"
	"github.com/gin-gonic/autotls"
	"github.com/gin-gonic/gin"
	uuid "github.com/satori/go.uuid"

	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/bbolt"

	_ "github.com/philippgille/gokv"
)

type userData_t struct {
	Id         string
	ForeignIDs []string
	Token      string
}

var gocial = gocialite.NewDispatcher()

//The url of the top of the authenticated part of your website
var baseUrl string = "https://entirety.praeceptamachinae.com/secure/"
var develop = false
var accessLog io.Writer
var db_prefix string = "authentigate_"
var providerSecrets map[string]map[string]string
var r *rand.Rand

type authDB struct {
	db                               *bolt.DB
	Users, ForeignIDs, SessionTokens gokv.Store
}

var b *authDB

//Turn errors into panics so we can catch them in the otp level handler and log them
func check(e error) {
	if e != nil {
		panic(e)
	}
}

//Create a log message in combined log format
func format_clf(c *gin.Context, id, responseCode, responseSize string) string {
	return fmt.Sprintf("%v - %v [%v] \"%v %v %v\" %v %v \"%v\" \"%v\"", c.ClientIP(), id, time.Now(), c.Request.Method, c.Request.RequestURI, c.Request.Proto, responseCode, responseSize, c.Request.Referer(), c.Request.UserAgent())
	//	127.0.0.1 - frank [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326 "http://www.example.com/start.html" "Mozilla/4.08 [en] (Win98; I ;Nav)"
}

//Check that the revocable session token is valid, load the user details, and call the provided handler for the url
//Or, redirect them to the login page
func makeAuthedRelay(handlerFunc func(*gin.Context, string, string, *Redirect, bool), relay *Redirect) func(c *gin.Context) {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Failure while processing %v: %v", c.Request.URL, r)
				log.Printf("Failure while processing %v: %v", c.Request.URL, r)
				debug.PrintStack()
				c.Status(500)
				displayPage(c, "", "files/BackendFailure.html", nil)
			}
		}()

		token, err := c.Cookie("AuthentigateSessionToken")
		useCookie := true
		if err == nil {
			log.Println("Found authentigate cookie")
		} else {
			log.Println("Cookie not found, using token")
			log.Println(err)
			token = c.Param("token")
			useCookie = false
		}
		id := sessionTokenToId(token)
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
	CopyHeaders []string
}
type Config struct {
	Redirects []Redirect
	Port      int
	BaseUrl   string
	HostNames []string
	LogFile   string
}

func main() {
	config := LoadConfig("config.json")
	if config.BaseUrl != "" {
		baseUrl = config.BaseUrl
	}
	var err error
	var f *os.File
	f, err = os.Create("accessLog")
	check(err)
	accessLog = bufio.NewWriter(f)
	flag.BoolVar(&develop, "develop", false, "Allow log in with no password")
	flag.StringVar(&baseUrl, "base-url", baseUrl, "The top level url for the authenticated section of your site")
	flag.Parse()

	var shutdownFunc func()
	b, err, shutdownFunc = newAuthDB(db_prefix)
	check(err)
	defer shutdownFunc()

	if develop {
		baseUrl = "http://localhost:80/secure/"
	}

	r = rand.New(rand.NewSource(time.Now().UnixNano()))
	router := gin.Default()

	router.GET("/", frontPageHandler)
	//User management pages
	router.GET("/manage/:token/token", makeAuthedRelay(tokenShowHandler, nil))
	router.GET("/manage/:token/newToken", makeAuthedRelay(newTokenHandler, nil))

	for _, loopPtr := range config.Redirects {
		relay := loopPtr
		fmt.Printf("Adding route from %v, to %v\n", relay.From, relay.To)
		switch relay.Tipe {
			case "GET":
			router.GET(relay.From, makeAuthedRelay(relayGetHandler, &relay))
			case "POST":
			router.POST(relay.From, makeAuthedRelay(relayPostHandler, &relay))
			case "PUT":
			router.PUT(relay.From, makeAuthedRelay(relayPutHandler, &relay))
		default:
			panic("Unsupported type for relay")
		}
	}

	//These are required to handle oauth2
	router.GET("/auth/:provider", redirectHandler)
	router.GET("/auth/:provider/callback", callbackHandler)

	//Drop CSS and js libraries in here
	router.Static("/files", "./files")
	if develop {
		router.GET("/develop/auth/callback", developCallbackHandler)
	}

	if develop {
		router.Run("127.0.0.1:80")
	} else {
		log.Fatal(autotls.Run(router, config.HostNames...))
	}
}

// Show homepage with login URL
func frontPageHandler(c *gin.Context) {
	extra := ""
	if develop {
		extra = "<hr/><a href='/develop/auth/callback'><button>Login with no password</button></a><br>"
	}
	c.Writer.Write([]byte("<html><head><title>Authentigate</title></head><body>" +
		"<a href='/auth/github'><button>Login with GitHub</button></a><br>" +
		//"<a href='/auth/linkedin'><button>Login with LinkedIn</button></a><br>" +
		"<a href='/auth/google'><button>Login with Google</button></a><br>" +
		"<a href='/auth/slack'><button>Login with Slack</button></a><br>" +
		"<hr/>"+
		"<a href='/auth/github'><button>Sign up with GitHub</button></a><br>" +
		//"<a href='/auth/linkedin'><button>Sign up with LinkedIn</button></a><br>" +
		"<a href='/auth/google'><button>Sign up with Google</button></a><br>" +
		"<a href='/auth/slack'><button>Sign up with Slack</button></a><br>" +
		extra +
		"</body></html>"))
	/*
		"<a href='/auth/amazon'><button>Login with Amazon</button></a><br>" +
		"<a href='/auth/bitbucket'><button>Login with Bitbucket</button></a><br>" +
		"<a href='/auth/facebook'><button>Login with Facebook</button></a><br>" +
	*/

}

//Given a session token, find the authentigate id
func sessionTokenToId(sessionToken string) string {
	var id string
	found, err := b.SessionTokens.Get(sessionToken, &id)
	if !found {
		log.Println("Could not find sessionToken", sessionToken, "in token store")
		return ""
	}
	check(err)

	if string(id) == "1" && !develop {
		panic("Invalid user id!  Id 1 is reserved for development")
	}
	return string(id)
}

//Load user data
func LoadUser(id string) *userData_t {
	var user userData_t
	found, err := b.Users.Get(id, &user)
	if err != nil || !found {
		return nil
	}

	return &user
}

//Given a provider id (string is provider name + provider id), find the authentigate id
func foreignIdToId(fid string) string {
	var id string
	found, err := b.ForeignIDs.Get(fid, &id)
	if !found {
		return ""
	}
	check(err)

	log.Printf("id %v from fid %v", id, fid)
	return string(id)
}

//Given an authentigate id, load the current session token
func idToSessionToken(id string) string {

	user := LoadUser(id)
	if user == nil {
		return ""
	}
	log.Printf("Token %v found for id %v", user.Token, id)
	return string(user.Token)
}

//Show the user their revocable token
func tokenShowHandler(c *gin.Context, blah string, token string, relay *Redirect, useCookie bool) {
	sessionID := c.Query("id")
	displayPage(c, sessionID, "files/showToken.html", nil)
}

//Show the user the successfull login message
func displayLoginPage(c *gin.Context, id string, sessionToken string) {

	displayPage(c,
		//Add switch here for cookie/url token mode
		"c",
		"files/loginSuccessful.html",
		map[string]string{"AuthentigateSessionToken": sessionToken})
}

func setupNewUser(c *gin.Context, foreignID string, token string) string {

	u1 := uuid.Must(uuid.NewV4())
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
	displayPage(c, sessionToken, "files/showToken.html", nil)
}

//Display a html file, inserting the revocable session token as needed
func displayPage(c *gin.Context, token, filename string, cookies map[string]string) {
	templateb, _ := ioutil.ReadFile(filename)
	template := string(templateb)
	template = templateSet(template, "TOKEN", token)
	template = templateSet(template, "BASE", baseUrl)
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

	//Copy the bare minimum needed for a post request
	CopyHeaders := []string{"Content-Type", "Content-Length"}
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

	//Write the result
	c.Writer.Write(respData)
	log.Printf("redirect PUT api %v, %v, %v\n", id, api, req.URL)
	accessLog.Write([]byte(format_clf(c, id, fmt.Sprint(resp.StatusCode), fmt.Sprint(resp.ContentLength)) + "\n"))
}

// Redirect to default microservice, using POST
func relayPostHandler(c *gin.Context, id, token string, relay *Redirect, useCookie bool) {

	api := c.Param("api")
	log.Printf("POST api : %v with id %v\n", api, id)
	bodyData, _ := ioutil.ReadAll(c.Request.Body)
	client := &http.Client{}
	uribits := strings.SplitN(c.Request.RequestURI, "?", 2)
	params := ""
	if len(uribits) > 1 {
		params = "?" + uribits[1]
	}

	req, err := http.NewRequest("POST", relay.To+api+params, nil)

	AddAuthToRequest(req, id, token, baseUrl, relay, useCookie)

	//Copy the bare minimum needed for a post request
	CopyHeaders := []string{"Content-Type", "Content-Length"}
	for _, h := range CopyHeaders {
		req.Header.Add(h, c.Request.Header.Get(h))
	}

	CopyHeaders = relay.CopyHeaders
	for _, h := range CopyHeaders {
		req.Header.Add(h, c.Request.Header.Get(h))
	}



	req.Body = ioutil.NopCloser(bytes.NewReader(bodyData))


	log.Printf("POST %v\n", req.URL)

	//Do it
	resp, err := client.Do(req)
	var respData []byte
	if resp != nil {
		if  resp.Body != nil {
			respData, err = ioutil.ReadAll(resp.Body)
			check(err)
		}

		//Copy back the bare minimum needed
		c.Header("Content-Type", resp.Header.Get("Content-Type"))
		c.Header("Content-Length", resp.Header.Get("Content-Length"))
	}

	//Write the result
	c.Writer.Write(respData)
	log.Printf("redirect POST api %v, %v, %v\n", id, api, req.URL)
	accessLog.Write([]byte(format_clf(c, id, fmt.Sprint(resp.StatusCode), fmt.Sprint(resp.ContentLength)) + "\n"))
}

//Not very functional, but it will do for now
func AddAuthToRequest(req *http.Request, id, token, baseUrl string, relay *Redirect, useCookie bool) {
	microserviceBaseUrl := fmt.Sprintf("%vc/%v/", baseUrl, relay.Name)
	microserviceTokenUrl := fmt.Sprintf("%v%v/%v/", baseUrl, token, relay.Name)
	siteTopUrl := fmt.Sprintf("%v%v/", baseUrl, token)
	req.Header.Add("authentigate-id", id)
	req.Header.Add("authentigate-token", token)
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


	CopyHeaders := relay.CopyHeaders
	for _, h := range CopyHeaders {
		req.Header.Add(h, c.Request.Header.Get(h))
	}

	log.Printf("redirect GET api %v, %v, %v\n", id, api, req.URL)
	resp, err := client.Do(req)
	check(err)
	respData, err := ioutil.ReadAll(resp.Body)

	c.Header("Content-Type", resp.Header.Get("Content-Type"))
	c.Writer.Write(respData)
	accessLog.Write([]byte(format_clf(c, id, fmt.Sprint(resp.StatusCode), fmt.Sprint(resp.ContentLength)) + "\n"))
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

//Wrap basic hash functions:  open/exists/put/get
//
//To switch to another keyval store, e.g. AWS, we just replace the API calls here
//Create and open the authentication keyval store
func newAuthDB(filename string) (s *authDB, err error, shutdownFunc func()) {
	s = &authDB{}
	s.db, err = bolt.Open(filename, 0600, &bolt.Options{Timeout: 1 * time.Second})

	options := bbolt.DefaultOptions
	options.Path = db_prefix + "users"
	options.BucketName = "users"
	s.Users, err = bbolt.NewStore(options)
	check(err)
	options = bbolt.DefaultOptions
	options.Path = db_prefix + "foreignIDs"
	options.BucketName = "foreignIDs"
	s.ForeignIDs, err = bbolt.NewStore(options)
	check(err)
	options = bbolt.DefaultOptions
	options.Path = db_prefix + "sessionTokens"
	options.BucketName = "sessionTokens"
	s.SessionTokens, err = bbolt.NewStore(options)
	check(err)

	s.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("users"))
		check(err)
		return nil
	})
	s.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("foreignIDs"))
		check(err)
		return nil
	})
	s.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("sessionTokens"))
		check(err)
		return nil
	})

	shutdownFunc = func() {
		defer s.Users.Close()

		defer s.ForeignIDs.Close()

		defer s.SessionTokens.Close()
	}
	return s, err, shutdownFunc
}

//Wrap basic hash functions:  exists/put/get
//
//To switch to another keyval store, e.g. AWS, we just replace the API calls here
func (s *authDB) Exists(bucket, key string) bool {
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

//Wrap basic hash functions:  exists/put/get
//
//To switch to another keyval store, e.g. AWS, we just replace the API calls here
func (s *authDB) Put(bucket, key string, val []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucket))
		check(err)
		b = tx.Bucket([]byte(bucket))
		if err = b.Put([]byte(key), val); err != nil {
			log.Printf("%v", err)
			panic(err)
		}

		return nil
	})
}

//Wrap basic hash functions:  exists/put/get
//
//To switch to another keyval store, e.g. AWS, we just replace the API calls here
func (s *authDB) Get(bucket, key string) (data []byte, err error) {
	err = errors.New("Id '" + key + "' not found!")
	s.db.View(func(tx *bolt.Tx) error {
		bb := tx.Bucket([]byte(bucket))
		r := bb.Get([]byte(key))
		if r != nil && len(r) > 0 {
			data = make([]byte, len(r))
			copy(data, r)
			err = nil
		}
		return nil
	})
	return
}

//Quick and dirty HTML templating
func templateSet(template, before, after string) string {
	template = strings.Replace(template, before, after, -1)
	return template
}

//Does the user exist in our user database?
func isNewUser(id string) bool {
	var user userData_t
	if found, _ := b.Users.Get(id, &user); found {
		return true
	} else {
		return false
	}
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

	displayLoginPage(c, id, token)

}

// Handle callback of providers - google, facebook etc
func developCallbackHandler(c *gin.Context) {
	if develop {

		id := "1"
		var token string
		if !b.Exists("users", id) {
			token = setupNewUser(c, id, "")
		} else {
			token = idToSessionToken(id)
		}
		if token == "" {
			token = setupNewUser(c, id, "")
		}

		displayLoginPage(c, id, token)
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
	c.Writer.Write(respData)
	accessLog.Write([]byte(format_clf(c, id, fmt.Sprint(resp.StatusCode), fmt.Sprint(resp.ContentLength)) + "\n"))
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
	accessLog.Write([]byte(format_clf(c, id, fmt.Sprint(resp.StatusCode), fmt.Sprint(resp.ContentLength)) + "\n"))
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
	c.Writer.Write(respData)
	log.Printf("redirect PUT api %v, %v, %v\n", id, api, req.URL)
	accessLog.Write([]byte(format_clf(c, id, fmt.Sprint(resp.StatusCode), fmt.Sprint(resp.ContentLength)) + "\n"))
}
