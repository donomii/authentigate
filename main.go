package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/donomii/gin"
	"github.com/gin-gonic/autotls"

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
	OTPWindowStart int64
	OTPFailures int
	OTPLastStep int64
}

var upGrader = websocket.Upgrader{CheckOrigin: sameWebsocketOrigin}

// The url of the top of the authenticated part of your website
var baseUrl string = "https://entirety.praeceptamachinae.com/secure/"
var develop = false
var accessLog io.Writer
var db_prefix string = "authentigate_"
var providerSecrets map[string]map[string]string



// Turn errors into panics so we can catch them in the otp level handler and log them
func check(e error) {
	if e != nil {
		panic(e)
	}
}

// Create a log message in combined log format
func format_clf(c *gin.Context, id, responseCode, responseSize string) string {
	return fmt.Sprintf("%v - %v [%v] \"%v %v %v\" %v %v \"-\" \"%v\"", c.RemoteIP(), id, time.Now(), c.Request.Method, c.FullPath(), c.Request.Proto, responseCode, responseSize, c.Request.UserAgent())
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
				logAuthFailure(c, fmt.Sprint(r))
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

		token, err := c.Cookie(authCookieName(c, "AuthentigateSessionToken"))
		useCookie := true
		if err != nil {
			token = c.Param("token")
			useCookie = false
		}
		id := sessionTokenToId(token)
		if id == "" {
			c.Status(http.StatusUnauthorized)
			frontPageHandler(c)
		} else {
			if useCookie && !validCookieOrigin(c) {
				return
			}
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
	MaxBodyBytes int64
	UpstreamTimeoutSeconds int
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
	f, err = os.OpenFile(config.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	check(err)
	defer f.Close()
	accessLog = f
	flag.BoolVar(&develop, "develop", false, "Allow log in with no password")
	flag.StringVar(&baseUrl, "base-url", baseUrl, "The top level url for the authenticated section of your site")
	flag.Parse()
	check(loadProviderSecrets("provider_secrets.json"))

	var shutdownFunc func()
	b, err, shutdownFunc = newAuthDB(db_prefix)
	check(err)
	defer shutdownFunc()

	if develop {
		baseUrl = "http://localhost:8000/secure/"
	}

	router := gin.New()
	router.Use(authRecovery)

	router.GET("/", frontPageHandler)
	//User management pages
	router.GET("/manage/:token/token", makeAuthedRelay(tokenShowHandler, nil, config.Bans))
	router.GET("/manage/:token/updateUser", makeAuthedRelay(updateUserHandler, nil, config.Bans))
	router.POST("/manage/:token/updateUser", makeAuthedRelay(updateUserHandler, nil, config.Bans))
	router.GET("/manage/:token/newToken", makeAuthedRelay(newTokenHandler, nil, config.Bans))
	router.POST("/manage/:token/newToken", makeAuthedRelay(newTokenHandler, nil, config.Bans))
	router.GET("/totplogin", totploginHandler)
	router.POST("/totplogin", totploginHandler)

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
	if c.Request.Method != http.MethodGet && !validManagementForm(c, token) {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	user := managementUser(c, id, token)
	if user == nil {
		return
	}

	if c.Request.Method == http.MethodGet {
		displayUserPage(c, user, "")
		return
	}
	username := c.Request.PostForm.Get("username")
	if username == "" {
		c.Status(http.StatusBadRequest)
		displayUserPage(c, user, "Username must not be empty")
		return
	}
	var owner string
	found, err := b.UserNames.Get(username, &owner)
	check(err)
	if found && owner != id {
		c.Status(http.StatusConflict)
		displayUserPage(c, user, "Username already exists")
		return
	}
	oldUsername := user.UserName
	user.UserName = username
	user.QRcode = generateTOTPWithSecret(user.OtpSecret, user.UserName)
	check(b.UserNames.Set(username, user.Id))
	SaveUser(user)
	if oldUsername != "" && oldUsername != username {
		if err := b.UserNames.Delete(oldUsername); err != nil {
			logUsernameCleanupFailure(user.Id, oldUsername, err)
		}
	}
	displayUserPage(c, user, "")
}

func totploginHandler(c *gin.Context) {
	if c.Request.Method == http.MethodGet {
		displayPage(c, "", "files/totplogin.html", nil, nil)
		return
	}
	if !allowOTPLogin(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	if err := c.Request.ParseForm(); err != nil {
		c.String(http.StatusBadRequest, "Unable to read authenticator login form")
		return
	}
	username := c.Request.PostForm.Get("username")
	code := c.Request.PostForm.Get("code")
	if username == "" || code == "" {
		c.String(http.StatusBadRequest, "Username and authenticator code are required")
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var userId string
	found, err := b.UserNames.Get(username, &userId)
	check(err)
	if !found {
		c.String(http.StatusUnauthorized, "Invalid username or authenticator code")
		return
	}
	user := LoadUser(userId)
	if user == nil || user.UserName != username || user.OtpSecret == "" || developmentUserDisabled(user) {
		c.String(http.StatusUnauthorized, "Invalid username or authenticator code")
		return
	}
	if !verifyUserOTP(c, user, code, time.Now().Unix()) {
		return
	}
	check(b.SessionTokens.Set(user.Token, user.Id))
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
	b.mu.Lock()
	defer b.mu.Unlock()
	user := managementUser(c, id, token)
	if user == nil {
		return
	}
	displayUserPage(c, user, "")
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
	user := userData_t{}
	user.Id = randomUserID()
	if foreignID == "1" && develop {
		user.Id = "1"
	}
	user.ForeignIDs = []string{foreignID}
	SaveUser(&user)
	user.Token = newTokenLocked(user.Id) //Newtoken also saves user data
	check(b.ForeignIDs.Set(foreignID, user.Id))

	return user.Token
}

func newTokenHandler(c *gin.Context, id string, token string, relay *Redirect, useCookie bool) {
	if c.Request.Method == http.MethodGet {
		tokenShowHandler(c, id, token, relay, useCookie)
		return
	}
	if !validManagementForm(c, token) {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if managementUser(c, id, token) == nil {
		return
	}
	sessionToken := newTokenLocked(id)
	setAuthCookie(c, "AuthentigateSessionToken", sessionToken, "/", 0)
	displayUserPage(c, LoadUser(id), "")
}

// Display a html file, inserting the revocable session token as needed
func displayPage(c *gin.Context, token, filename string, cookies map[string]string, subs map[string]string) {
	templateb, err := ioutil.ReadFile(filename)
	check(err)
	for cookieName, cookieValue := range cookies {
		setAuthCookie(c, cookieName, cookieValue, "/", 0)
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Writer.Write([]byte(renderAuthPage(string(templateb), token, subs)))
}

// Redirect to default microservice, using PUT
func relayPutHandler(c *gin.Context, id, token string, relay *Redirect, useCookie bool) {
	relayHTTPHandler(c, id, token, relay, useCookie)
}

// Redirect to default microservice, using POST
func relayPostHandler(c *gin.Context, id, token string, relay *Redirect, useCookie bool) {
	relayHTTPHandler(c, id, token, relay, useCookie)
}

// Not very functional, but it will do for now
func AddAuthToRequest(req *http.Request, id, token, baseUrl string, relay *Redirect, useCookie bool) {
	microserviceBaseUrl := fmt.Sprintf("%vc/%v/", baseUrl, relay.Name)
	microserviceTokenUrl := fmt.Sprintf("%v%v/%v/", baseUrl, token, relay.Name)
	siteTopUrl := fmt.Sprintf("%v%v/", baseUrl, token)
	req.Header.Set("authentigate-id", id)
	req.Header.Set("authentigate-token", token)
	//req.Header.Add("X-Auth-Token", token)
	req.Header.Set("authentigate-base-url", microserviceBaseUrl)
	req.Header.Set("authentigate-base-token-url", microserviceTokenUrl)
	req.Header.Set("authentigate-top-url", siteTopUrl)
}

// Redirect to default microservice, using GET
func relayGetHandler(c *gin.Context, id, token string, relay *Redirect, useCookie bool) {
	relayHTTPHandler(c, id, token, relay, useCookie)
}

// Redirect to correct oAuth URL
func redirectHandler(c *gin.Context) {
	// Retrieve provider from route
	provider := c.Param("provider")
	// In this case we use a map to store our secrets
	authURL, err := beginOAuth(c, provider)
	// Check for errors (usually driver not valid)
	if err != nil {
		c.String(http.StatusBadRequest, "Unable to start OAuth login: %s", err)
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
	uri := totp.ProvisioningUri(username, "Authentigate")
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
	b.mu.Lock()
	defer b.mu.Unlock()
	return newTokenLocked(id)
}

func newTokenLocked(id string) string {
	user := LoadUser(id)
	if user == nil {
		panic("Cannot rotate token: user not found for id " + id)
	}
	sessionToken := randomSessionToken()
	oldToken := user.Token
	user.Token = sessionToken
	if user.OtpSecret == "" {
		user.OtpSecret = randomOTPSecret()
	}
	check(b.SessionTokens.Set(sessionToken, id))
	SaveUser(user)
	if oldToken != "" {
		if err := b.SessionTokens.Delete(oldToken); err != nil {
			logTokenCleanupFailure(id, oldToken, err)
		}
	}
	return sessionToken
}

// Handle callback of providers - google, facebook etc
func callbackHandler(c *gin.Context) {
	// Retrieve query params for state and code
	state := c.Query("state")
	code := c.Query("code")
	provider := c.Param("provider")
	// Handle callback and check for errors
	flow, libraryState, err := consumeOAuth(c, provider, state)
	if err != nil {
		c.String(http.StatusBadRequest, "OAuth login rejected: %s", err)
		return
	}
	if err := flow.Handle(libraryState, code); err != nil {
		logAuthFailure(c, fmt.Sprintf("OAuth provider %s failed: %v", provider, err))
		c.String(http.StatusBadGateway, "OAuth provider %s could not complete login: %s", provider, authErrorDetail(c, err.Error()))
		return
	}
	if flow.User.ID == "" {
		c.String(http.StatusBadGateway, "OAuth provider %s returned no account ID", provider)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	foreignID := provider + flow.User.ID
	id := foreignIdToId(foreignID)
	var token string
	if id == "" {
		//First time user.  Create an account for them, because they have already gone through the authentication process using OAuth2
		token = setupNewUser(c, foreignID, "")
	} else {
		user := LoadUser(id)
		if user == nil || developmentUserDisabled(user) {
			c.String(http.StatusInternalServerError, "OAuth account has no valid local user")
			return
		}
		token = user.Token
		check(b.SessionTokens.Set(token, id))
	}

	displayLoginSuccessfulPage(c, id, token)

}

// Handle callback of providers - google, facebook etc
func developCallbackHandler(c *gin.Context) {
	if develop {
		b.mu.Lock()
		defer b.mu.Unlock()
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

		displayLoginSuccessfulPage(c, id, token)
	} else {
		c.String(http.StatusForbidden, "Development login is disabled")
	}
}

//Custom fileserver relay
//
//Eventually, combine this with the rest of the code using configuration files

func ngfileserverRelayParameterisedHandler(c *gin.Context, id, token string, relay *Redirect, useCookie bool) {
	c.Header("Cache-Control", "no-cache,no-store")
	relayHTTPHandler(c, id, token, relay, useCookie)
}

func ngfileserverRelayHandler(c *gin.Context, id, token, target string) {
	relay := &Redirect{Name: "ngfileserver", To: target}
	relayHTTPHandler(c, id, token, relay, false)
}

func ngfileserverPutRelayHandler(c *gin.Context, id, token, target string) {
	relay := &Redirect{Name: "ngfileserver", To: target}
	relayHTTPHandler(c, id, token, relay, false)
}
