package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"crypto/tls"

	"github.com/danilopolani/gocialite"
	uuid "github.com/satori/go.uuid"
	"golang.org/x/crypto/acme/autocert"

	"github.com/gorilla/websocket"

	_ "github.com/philippgille/gokv"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/xlzd/gotp"
)

type userData_t struct {
	Id             string
	ForeignIDs     []string
	Token          string
	QRcode         []byte
	OtpSecret      string
	OTPWindowStart int64
	OTPFailures    int
	OTPLastStep    int64
	UserName       string
}

type Context struct {
	Writer  http.ResponseWriter
	Request *http.Request
	params  map[string]string
}

func (c *Context) Param(key string) string {
	// For Go 1.22+, we can use r.PathValue
	return c.Request.PathValue(key)
}

func (c *Context) Query(key string) string {
	return c.Request.URL.Query().Get(key)
}

func (c *Context) Status(code int) {
	c.Writer.WriteHeader(code)
}

func (c *Context) Header(key, value string) {
	c.Writer.Header().Set(key, value)
}

func (c *Context) RemoteIP() string {
	return c.Request.RemoteAddr
}

func (c *Context) ClientIP() string {
	return c.Request.RemoteAddr
}

func (c *Context) JSON(code int, obj interface{}) {
	c.Header("Content-Type", "application/json")
	c.Status(code)
	json.NewEncoder(c.Writer).Encode(obj)
}

func (c *Context) String(code int, format string, values ...interface{}) {
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Status(code)
	fmt.Fprintf(c.Writer, format, values...)
}

func (c *Context) Redirect(code int, location string) {
	http.Redirect(c.Writer, c.Request, location, code)
}

func (c *Context) Cookie(name string) (string, error) {
	cookie, err := c.Request.Cookie(name)
	if err != nil {
		return "", err
	}
	return cookie.Value, nil
}

func (c *Context) SetCookie(name, value string, maxAge int, path, domain string, secure, httpOnly bool) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    value,
		MaxAge:   maxAge,
		Path:     path,
		Domain:   domain,
		Secure:   secure,
		HttpOnly: httpOnly,
	})
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
var oauthEnabled = false

// Turn errors into panics so we can catch them in the otp level handler and log them
func check(e error) {
	if e != nil {
		panic(e)
	}
}

// Create a log message in combined log format
func format_clf(c *Context, id, responseCode, responseSize string) string {
	return fmt.Sprintf("%v - %v [%v] \"%v %v %v\" %v %v \"-\" \"%v\"", c.RemoteIP(), id, time.Now(), c.Request.Method, c.Request.Pattern, c.Request.Proto, responseCode, responseSize, c.Request.UserAgent())
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
func makeAuthedRelay(handlerFunc func(*Context, string, string, *Redirect, bool), relay *Redirect, bans []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := &Context{Writer: w, Request: r}
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
	OAuthEnabled bool
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
	oauthEnabled = config.OAuthEnabled
	var err error
	var f *os.File
	f, err = os.Create("accessLog")
	check(err)
	accessLog = bufio.NewWriterSize(f, 999999) //Golang!
	flag.BoolVar(&develop, "develop", false, "Allow log in with no password")
	flag.StringVar(&baseUrl, "base-url", baseUrl, "The top level url for the authenticated section of your site")
	flag.Parse()
	if oauthEnabled {
		check(loadProviderSecrets("provider_secrets.json"))
	}

	var shutdownFunc func()
	b, err, shutdownFunc = newAuthDB(db_prefix)
	check(err)
	defer shutdownFunc()

	if develop {
		baseUrl = "http://localhost:8000/secure/"
	}

	r = rand.New(rand.NewSource(time.Now().UnixNano()))
	mux := http.NewServeMux()

	// Static files - register first to avoid conflicts
	mux.Handle("GET /files/", http.StripPrefix("/files/", http.FileServer(http.Dir("./files"))))

	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./favicon.ico")
	})

	// Front page - exact match only
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		frontPageHandler(&Context{Writer: w, Request: r})
	})

	// User management pages
	mux.HandleFunc("GET /manage/{token}/token", makeAuthedRelay(tokenShowHandler, nil, config.Bans))
	mux.HandleFunc("GET /manage/{token}/updateUser", makeAuthedRelay(updateUserHandler, nil, config.Bans))
	mux.HandleFunc("POST /manage/{token}/updateUser", makeAuthedRelay(updateUserHandler, nil, config.Bans))
	mux.HandleFunc("GET /manage/{token}/newToken", makeAuthedRelay(newTokenHandler, nil, config.Bans))
	mux.HandleFunc("POST /manage/{token}/newToken", makeAuthedRelay(newTokenHandler, nil, config.Bans))
	mux.HandleFunc("GET /totplogin", func(w http.ResponseWriter, r *http.Request) {
		totploginHandler(&Context{Writer: w, Request: r}, "", "", nil, false)
	})
	mux.HandleFunc("POST /totplogin", func(w http.ResponseWriter, r *http.Request) {
		totploginHandler(&Context{Writer: w, Request: r}, "", "", nil, false)
	})
	mux.HandleFunc("GET /totpsignup", func(w http.ResponseWriter, r *http.Request) {
		totpsignupHandler(&Context{Writer: w, Request: r})
	})
	mux.HandleFunc("POST /totpsignup", func(w http.ResponseWriter, r *http.Request) {
		totpsignupHandler(&Context{Writer: w, Request: r})
	})

	for _, loopPtr := range config.Redirects {
		relay := loopPtr
		fmt.Printf("Adding route from %v, to %v\n", relay.From, relay.To)
		// Convert gin-style :param to mux-style {param}
		// and gin-style *api to mux-style {api...}
		pattern := relay.From
		// First handle wildcards like *api -> {api...}
		if strings.Contains(pattern, "*") {
			pattern = strings.Replace(pattern, "*", "{", 1)
			pattern = pattern + "...}"
		}
		// Then handle path params like :token -> {token}
		for strings.Contains(pattern, ":") {
			start := strings.Index(pattern, ":")
			end := start + 1
			for end < len(pattern) && pattern[end] != '/' {
				end++
			}
			paramName := pattern[start+1 : end]
			pattern = pattern[:start] + "{" + paramName + "}" + pattern[end:]
		}

		switch relay.Tipe {
		case "GET":
			mux.HandleFunc("GET "+pattern, makeAuthedRelay(relayGetHandler, &relay, config.Bans))
		case "POST":
			mux.HandleFunc("POST "+pattern, makeAuthedRelay(relayPostHandler, &relay, config.Bans))
		case "PUT":
			mux.HandleFunc("PUT "+pattern, makeAuthedRelay(relayPutHandler, &relay, config.Bans))
		default:
			panic("Unsupported type for relay")
		}
	}

	if oauthEnabled {
		// These are required to handle oauth2
		mux.HandleFunc("GET /auth/{provider}", func(w http.ResponseWriter, r *http.Request) {
			redirectHandler(&Context{Writer: w, Request: r})
		})
		mux.HandleFunc("GET /auth/{provider}/callback", func(w http.ResponseWriter, r *http.Request) {
			callbackHandler(&Context{Writer: w, Request: r})
		})
	} else {
		// Explicitly disable OAuth endpoints when turned off.
		mux.HandleFunc("GET /auth/{provider}", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		mux.HandleFunc("GET /auth/{provider}/callback", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
	}

	if develop {
		mux.HandleFunc("GET /develop/auth/callback", func(w http.ResponseWriter, r *http.Request) {
			developCallbackHandler(&Context{Writer: w, Request: r})
		})
	}

	if develop {
		addr := fmt.Sprintf("127.0.0.1:%d", config.Port)
		fmt.Printf("Starting in development mode on %s...\n", addr)
		log.Fatal(http.ListenAndServe(addr, authRecovery(mux)))
	} else {
		if config.Secure {
			fmt.Printf("Starting in production mode with HTTPS on :443 (TLS) and :%d (ACME challenges)...\n", config.Port)
			// Configure autocert manager for Let's Encrypt
			certManager := autocert.Manager{
				Prompt:     autocert.AcceptTOS,
				HostPolicy: autocert.HostWhitelist(config.HostNames...),
				Cache:      autocert.DirCache(".certs"), // Store certs in .certs directory
			}

			// HTTPS server on port 443.
			// Use certManager.TLSConfig() so that the ACME TLS-ALPN-01 challenge
			// handler is wired in (sets NextProtos to include "acme-tls/1").
			tlsCfg := certManager.TLSConfig()
			tlsCfg.MinVersion = tls.VersionTLS12
			server := &http.Server{
				Addr:      ":443",
				Handler:   authRecovery(mux),
				TLSConfig: tlsCfg,
			}

			// HTTP server on config.Port - serves requests AND handles ACME challenges
			go func() {
				h := certManager.HTTPHandler(authRecovery(mux))
				addr := fmt.Sprintf(":%d", config.Port)
				fmt.Printf("HTTP listening on %s\n", addr)
				log.Fatal(http.ListenAndServe(addr, h))
			}()

			log.Fatal(server.ListenAndServeTLS("", "")) // Certs provided by autocert
		} else {
			addr := fmt.Sprintf(":%d", config.Port)
			fmt.Printf("Starting in production mode on %s...\n", addr)
			log.Fatal(http.ListenAndServe(addr, authRecovery(mux)))
		}
	}
}

func updateUserHandler(c *Context, id string, token string, relay *Redirect, useCookie bool) {
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

func totploginHandler(c *Context, id string, token string, relay *Redirect, useCookie bool) {
	if c.Request.Method == http.MethodGet {
		displayPage(c, "", "files/totplogin.html", nil, nil)
		return
	}
	if !validCookieOrigin(c) {
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
		ps := loadPending()
		pending, ok := ps.get(username)
		if !ok {
			c.String(http.StatusUnauthorized, "Invalid username or authenticator code")
			return
		}
		if !pending.Approved {
			displayPage(c, "", "files/totppending.html", nil, nil)
			return
		}
		if !gotp.NewDefaultTOTP(pending.OtpSecret).Verify(code, time.Now().Unix()) {
			c.String(http.StatusUnauthorized, "Invalid username or authenticator code")
			return
		}
		sessionToken := setupTOTPUser(c, username, pending.OtpSecret)
		var createdID string
		_, err := b.UserNames.Get(username, &createdID)
		check(err)
		created := LoadUser(createdID)
		created.OTPLastStep = time.Now().Unix() / 30
		SaveUser(created)
		check(ps.remove(username))
		displayLoginSuccessfulPage(c, created.Id, sessionToken)
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

// Promote an approved pending TOTP user to a full user account
func setupTOTPUser(c *Context, username, otpSecret string) string {
	u1 := uuid.NewV4()
	user := userData_t{}
	user.Id = u1.String()
	user.UserName = username
	user.OtpSecret = otpSecret
	user.QRcode = generateTOTPWithSecret(otpSecret, username)
	SaveUser(&user)
	user.Token = newTokenLocked(user.Id)
	b.UserNames.Set(username, user.Id)
	return user.Token
}

func totpsignupHandler(c *Context) {
	r := c.Request
	w := c.Writer
	if err := r.ParseForm(); err != nil {
		fmt.Fprintf(w, "ParseForm() err: %v", err)
		return
	}
	username := r.FormValue("username")
	const signupForm = `<form action="/totpsignup" method="post">
  <p>Username: <input type="text" name="username"/></p>
  <p><input type="submit" value="Sign up"/></p>
</form>`

	if username == "" {
		displayPage(c, "", "files/totpsignup.html", nil, map[string]string{"ERROR": "", "MESSAGE": "", "QRCODE_BLOCK": "", "SIGNUP_FORM": signupForm})
		return
	}

	// Check username not already taken by a full user
	var existingId string
	found, err := b.UserNames.Get(username, &existingId)
	if err != nil {
		fmt.Fprintf(w, "Error checking username: %v", err)
		return
	}
	if found {
		displayPage(c, "", "files/totpsignup.html", nil, map[string]string{"ERROR": "Username already taken", "MESSAGE": "", "QRCODE_BLOCK": "", "SIGNUP_FORM": signupForm})
		return
	}

	// Check not already pending
	ps := loadPending()
	if ps.exists(username) {
		displayPage(c, "", "files/totpsignup.html", nil, map[string]string{"ERROR": "Username already registered and awaiting approval", "MESSAGE": "", "QRCODE_BLOCK": "", "SIGNUP_FORM": signupForm})
		return
	}

	// Generate secret and QR code
	otpSecret := gotp.RandomSecret(16)
	qrPng := generateTOTPWithSecret(otpSecret, username)
	qrDataURI := fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(qrPng))

	if err := ps.add(username, otpSecret); err != nil {
		fmt.Fprintf(w, "Error saving signup: %v", err)
		return
	}

	log.Printf("New TOTP signup pending approval: %s", username)
	displayPage(c, "", "files/totpsignup.html", nil, map[string]string{
		"ERROR":        "",
		"MESSAGE":      "<strong>Scan the QR code now with your authenticator app — it will not be shown again.</strong><br/>Once scanned, wait for approval before logging in.",
		"QRCODE_BLOCK": fmt.Sprintf("<img src='%s'/><br/>", qrDataURI),
		"SIGNUP_FORM":  "",
	})
}

// Show homepage with login URL
func frontPageHandler(c *Context) {
	subs := map[string]string{}
	if develop {

		subs["DEVELOPER"] = "<hr/><a href='/develop/auth/callback'><button>Login with no password</button></a><br>"
	} else {
		subs["DEVELOPER"] = ""
	}
	displayPage(c, "", "files/frontpage.html", nil, subs)
}

// Show the user their revocable token
func tokenShowHandler(c *Context, id string, token string, relay *Redirect, useCookie bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	user := managementUser(c, id, token)
	if user == nil {
		return
	}
	displayUserPage(c, user, "")
}

// Show the user the successful login message
func displayLoginSuccessfulPage(c *Context, id string, sessionToken string) {
	displayPage(c,
		//Add switch here for cookie/url token mode
		sessionToken,
		"files/loginSuccessful.html",
		map[string]string{"AuthentigateSessionToken": sessionToken},
		nil)
}

func setupNewUser(c *Context, foreignID string, token string) string {
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

func newTokenHandler(c *Context, id string, token string, relay *Redirect, useCookie bool) {
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
func displayPage(c *Context, token, filename string, cookies map[string]string, subs map[string]string) {
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
func relayPutHandler(c *Context, id, token string, relay *Redirect, useCookie bool) {

	api := "/" + c.Param("api")
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

	req.Body = io.NopCloser(bytes.NewReader(bodyData))

	log.Printf("PUT %v\n", req.URL)

	//Do it
	resp, err := client.Do(req)
	check(err)
	respData, err := io.ReadAll(resp.Body)
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
func relayPostHandler(c *Context, id, token string, relay *Redirect, useCookie bool) {

	api := "/" + c.Param("api")
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
	req.Body = io.NopCloser(bytes.NewReader(bodyData))

	log.Printf("POST %v\n", req.URL)

	//Do it
	resp, err := client.Do(req)
	var respData []byte
	if resp != nil {
		if resp.Body != nil {
			respData, err = io.ReadAll(resp.Body)
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
		log.Printf("redirect failed %+v\n", resp)

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

// isStreamingContentType returns true for content types that must be proxied
// as a live stream rather than buffered (e.g. MJPEG multipart responses).
func isStreamingContentType(ct string) bool {
	return strings.HasPrefix(ct, "multipart/x-mixed-replace")
}

// relayStreamHandler proxies a streaming response (e.g. MJPEG) without buffering.
// It copies bytes as they arrive and flushes after each chunk.
func relayStreamHandler(c *Context, id, token string, relay *Redirect, useCookie bool, req *http.Request, resp *http.Response) {
	defer resp.Body.Close()

	for k, vs := range resp.Header {
		for _, v := range vs {
			c.Writer.Header().Add(k, v)
		}
	}
	c.Writer.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Writer.Header().Set("Pragma", "no-cache")
	c.Writer.WriteHeader(resp.StatusCode)

	flusher, canFlush := c.Writer.(http.Flusher)

	ctx := c.Request.Context()
	go func() {
		<-ctx.Done()
		resp.Body.Close()
	}()

	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := c.Writer.Write(buf[:n]); writeErr != nil {
				if ctx.Err() == nil {
					log.Printf("relayStreamHandler: write failed for %v: %v", req.URL, writeErr)
				}
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr != nil {
			if readErr != io.EOF && ctx.Err() == nil {
				log.Printf("relayStreamHandler: read ended for %v: %v", req.URL, readErr)
			}
			return
		}
	}
}

// Redirect to default microservice, using GET
func relayGetHandler(c *Context, id, token string, relay *Redirect, useCookie bool) {

	api := "/" + c.Param("api")

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

	// If the backend is sending a streaming response (e.g. MJPEG), proxy it
	// as a live stream rather than buffering the whole body.
	if isStreamingContentType(resp.Header.Get("Content-Type")) {
		log.Printf("relayGetHandler: streaming response for %v", req.URL)
		relayStreamHandler(c, id, token, relay, useCookie, req, resp)
		accessLog.Write([]byte(format_clf(c, id, "200", "-") + "\n"))
		return
	}

	respData, err := io.ReadAll(resp.Body)

	c.Header("Content-Type", resp.Header.Get("Content-Type"))
	c.Header("Content-Disposition", resp.Header.Get("Content-Disposition"))
	c.Status(resp.StatusCode)
	c.Writer.Write(respData)
	logMess := []byte(format_clf(c, id, fmt.Sprintf("%v", resp.StatusCode), fmt.Sprintf("%v", resp.ContentLength)) + "\n")
	accessLog.Write(logMess)
}

// Redirect to correct oAuth URL
func redirectHandler(c *Context) {
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

// Handle callback of providers - google, facebook etc
func callbackHandler(c *Context) {
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
func developCallbackHandler(c *Context) {
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

func ngfileserverRelayParameterisedHandler(c *Context, id, token string, relay *Redirect, useCookie bool) {
	c.Header("Cache-Control", "no-cache,no-store")
	api := "/" + c.Param("api")
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
	respData, err := io.ReadAll(resp.Body)
	check(err)
	c.Header("Content-Type", resp.Header.Get("Content-Type"))
	c.Status(resp.StatusCode)
	c.Writer.Write(respData)
	accessLog.Write([]byte(format_clf(c, id, fmt.Sprintf("%v", resp.StatusCode), fmt.Sprintf("%v", resp.ContentLength)) + "\n"))
}

func ngfileserverRelayHandler(c *Context, id, token, target string) {

	api := "/" + c.Param("api")
	log.Printf("api call: %v with id %v\n", api, id)

	completeBaseUrl := fmt.Sprintf("%v%v/ngfileserver/", baseUrl, token)

	client := &http.Client{}

	req, err := http.NewRequest("GET", target+api, nil)

	req.Header.Add("authentigate-id", id)
	req.Header.Add("authentigate-token", token)
	req.Header.Add("authentigate-base-url", completeBaseUrl)
	log.Printf("Relaying call to %v\n", req.URL)
	resp, err := client.Do(req)
	respData, err := io.ReadAll(resp.Body)
	check(err)
	c.Writer.Write(respData)
	accessLog.Write([]byte(format_clf(c, id, fmt.Sprintf("%v", resp.StatusCode), fmt.Sprintf("%v", resp.ContentLength)) + "\n"))
}

func ngfileserverPutRelayHandler(c *Context, id, token, target string) {

	api := "/" + c.Param("api")
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
	req.Body = io.NopCloser(bytes.NewReader(bodyData))
	log.Printf("Relaying put call to %v\n", req.URL)
	resp, err := client.Do(req)
	respData, err := io.ReadAll(resp.Body)
	check(err)
	c.Header("Content-Type", resp.Header.Get("Content-Type"))
	c.Status(resp.StatusCode)
	c.Writer.Write(respData)
	log.Printf("redirect PUT api %v, %v, %v\n", id, api, req.URL)
	accessLog.Write([]byte(format_clf(c, id, fmt.Sprintf("%v", resp.StatusCode), fmt.Sprintf("%v", resp.ContentLength)) + "\n"))
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
