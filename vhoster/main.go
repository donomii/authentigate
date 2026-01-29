package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

//The url of the top of the authenticated part of your website

var develop = false
var accessLog *bufio.Writer
var db_prefix string = "authentigate_"
var providerSecrets map[string]map[string]string
var r *rand.Rand

type MyContext struct {
	Request http.Request
	Writer  http.ResponseWriter
}

func (m *MyContext) ClientIP() string {
	return m.Request.RemoteAddr
}

func (m *MyContext) RemoteIP() string {
	return m.Request.RemoteAddr
}

func (m *MyContext) Status(code int) {
	m.Writer.WriteHeader(code)
}

func (m *MyContext) Header(key, value string) {
	m.Writer.Header().Set(key, value)
}

type MyMux struct {
}

func (m MyMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c := MyContext{Writer: w, Request: *r}
	logMess := format_clf(&c, "", "???", "???"+"\n")
	fmt.Println(logMess)

	handled := false
	var triedRoutes []string

	switch r.Method {
	case "GET":
		log.Println("GET method")
		for _, v := range config.Redirects {
			triedRoutes = append(triedRoutes, fmt.Sprintf("'%s' (suffix match against '%s')", v.Host, c.Request.Host))
			if strings.HasSuffix(c.Request.Host, v.Host) {
				log.Printf("✓ Matched route: %s -> %s\n", v.Host, v.To)
				relayGetHandler(&c, &v)
				handled = true
				return
			}
		}
	case "POST":
		log.Println("POST method")
		for _, v := range config.Redirects {
			triedRoutes = append(triedRoutes, fmt.Sprintf("'%s' (suffix match against '%s')", v.Host, c.Request.Host))
			if strings.HasSuffix(c.Request.Host, v.Host) {
				log.Printf("✓ Matched route: %s -> %s\n", v.Host, v.To)
				relayPostHandler(&c, &v)
				handled = true
				return
			}
		}
	case "PUT":
		log.Println("PUT method")
		for _, v := range config.Redirects {
			triedRoutes = append(triedRoutes, fmt.Sprintf("'%s' (suffix match against '%s')", v.Host, c.Request.Host))
			if strings.HasSuffix(c.Request.Host, v.Host) {
				log.Printf("✓ Matched route: %s -> %s\n", v.Host, v.To)
				relayPutHandler(&c, &v)
				handled = true
				return
			}
		}
	}

	if !handled {
		log.Printf("✗ No route found for host: '%s', method: %s\n", c.Request.Host, r.Method)
		log.Printf("  Tried routes:\n")
		for _, route := range triedRoutes {
			log.Printf("    - %s\n", route)
		}
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404 - No route configured for this host"))
	}
}

// Turn errors into panics so we can catch them in the otp level handler and log them
func check(e error) {
	if e != nil {
		panic(e)
	}
}

// Create a log message in combined log format
func format_clf(c *MyContext, id, responseCode, responseSize string) string {
	return fmt.Sprintf("%v - %v [%v] \"%v %v %v\" %v %v \"%v\" \"%v\"", c.ClientIP(), id, time.Now(), c.Request.Method, c.Request.RequestURI, c.Request.Proto, responseCode, responseSize, c.Request.Referer(), c.Request.UserAgent())
	//	127.0.0.1 - frank [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326 "http://www.example.com/start.html" "Mozilla/4.08 [en] (Win98; I ;Nav)"
}

type Redirect struct {
	Host, To, Name string
	CopyHeaders    []string
}
type Config struct {
	Redirects []Redirect
	Port      int

	HostNames []string
	LogFile   string
}

var config *Config

func main() {
	// Parse command line flags first
	var configFile, logFile string
	flag.StringVar(&configFile, "c", "config.json", "Path to config file")
	flag.StringVar(&configFile, "config", "config.json", "Path to config file")
	flag.StringVar(&logFile, "l", "accessLog", "Path to access log file")
	flag.StringVar(&logFile, "log", "accessLog", "Path to access log file")
	flag.BoolVar(&develop, "develop", false, "Development mode (no HTTPS, port 3000)")
	flag.Parse()

	// Load config
	config = LoadConfig(configFile)
	if config == nil {
		fmt.Printf("Failed to read config file '%s', exiting!\n", configFile)
		os.Exit(1)
	}

	// Set up access log
	var err error
	var f *os.File
	// Allow LOG_FILE env var to override flag
	if envLog := os.Getenv("LOG_FILE"); envLog != "" {
		logFile = envLog
	}
	f, err = os.Create(logFile)
	check(err)
	accessLog = bufio.NewWriterSize(f, 999999)

	// Print routing configuration on startup
	fmt.Println("=== vhoster routing configuration ===")
	fmt.Printf("Port: %d\n", config.Port)
	fmt.Printf("Development mode: %v\n", develop)
	fmt.Printf("Routes (%d configured):\n", len(config.Redirects))
	for i, r := range config.Redirects {
		fmt.Printf("  [%d] Host: %s -> To: %s (Name: %s)\n", i, r.Host, r.To, r.Name)
	}
	fmt.Println("=====================================")

	r = rand.New(rand.NewSource(time.Now().UnixNano()))
	mux := MyMux{}

	if develop {
		fmt.Printf("Starting in development mode on port 3000...\n")
		http.ListenAndServe(":3000", mux)
	} else {
		fmt.Println("Starting in production mode...")
		fmt.Println("  HTTP  on :80  (proxying + ACME challenges)")
		fmt.Println("  HTTPS on :443 (Let's Encrypt auto-certs)")

		// Configure autocert manager for Let's Encrypt
		certManager := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(config.HostNames...),
			Cache:      autocert.DirCache(".certs"), // Store certs in .certs directory
		}

		// HTTPS server on port 443
		server := &http.Server{
			Addr:    ":443",
			Handler: mux,
			TLSConfig: &tls.Config{
				GetCertificate: certManager.GetCertificate,
				MinVersion:     tls.VersionTLS12,
			},
		}

		// HTTP server on port 80 - serves requests AND handles ACME challenges
		go func() {
			// HTTPHandler wraps our mux to also handle ACME challenges
			h := certManager.HTTPHandler(mux)
			log.Fatal(http.ListenAndServe(":80", h))
		}()

		log.Fatal(server.ListenAndServeTLS("", "")) // Certs provided by autocert
	}
}

// Redirect to default microservice, using PUT
func relayPutHandler(c *MyContext, relay *Redirect) {

	bodyData, _ := io.ReadAll(c.Request.Body)
	client := &http.Client{}
	path := c.Request.URL.Path

	req, err := http.NewRequest("PUT", relay.To+path, nil)

	req.Header.Add("X-Forwarded-For", c.Request.RemoteAddr)
	req.Header.Add("X-Real-IP", c.RemoteIP())

	for k, v := range c.Request.Header {
		req.Header[k] = v
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

	//Write the result
	c.Writer.Write(respData)
	log.Printf("redirect PUT  %v\n", req.URL)
	accessLog.Write([]byte(format_clf(c, "", fmt.Sprintf("%v", resp.StatusCode), fmt.Sprintf("%v", resp.ContentLength)) + "\n"))
	accessLog.Flush()
}

// Redirect to default microservice, using POST
func relayPostHandler(c *MyContext, relay *Redirect) {

	path := c.Request.URL.Path
	bodyData, err := io.ReadAll(c.Request.Body)
	if err != nil {
		panic(err)
	}
	client := &http.Client{}

	req, err := http.NewRequest("POST", relay.To+path, nil)

	req.Header.Add("X-Forwarded-For", c.Request.RemoteAddr)
	req.Header.Add("X-Real-IP", c.Request.RemoteAddr)

	CopyHeaders := relay.CopyHeaders
	log.Printf("Forwarding headers %v\n", CopyHeaders)
	log.Printf("Request %+v\n", c)
	log.Printf("Request body %v\n", string(bodyData))
	for k, _ := range c.Request.Header {
		req.Header.Add(k, c.Request.Header.Get(k))
		log.Printf("Copyheader: %v, %v\n", k, c.Request.Header.Get(k))
	}

	req.Header.Add("X-Forwarded-Port", fmt.Sprintf("%v", config.Port))
	req.Header.Add("X-Forwarded-Proto", c.Request.Proto)
	req.Header.Add("X-Forwarded-For", c.Request.RemoteAddr)
	req.Header.Add("X-Real-IP", c.RemoteIP())
	req.Header.Add("Host", "entirety.praeceptamachinae.com")

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
		log.Printf("redirect POST %v\n", req.URL)
		accessLog.Write([]byte(format_clf(
			c,
			"",
			fmt.Sprintf("%v", resp.StatusCode),
			fmt.Sprintf("%v", resp.ContentLength)) + "\n"))
	} else {
		log.Printf("redirect POST %v\n", req.URL)
		log.Printf("redirect failed %+v\n", resp)

	}
	accessLog.Flush()
}

// Redirect to default microservice, using GET
func relayGetHandler(c *MyContext, relay *Redirect) {

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	path := c.Request.URL.RequestURI()

	//TODO make this configurable from a file
	req, err := http.NewRequest("GET", relay.To+path, nil)

	req.Header.Add("X-Forwarded-For", c.Request.RemoteAddr)
	req.Header.Add("X-Real-IP", c.RemoteIP())

	for k, v := range c.Request.Header {
		req.Header[k] = v
	}

	log.Printf("redirect GET %v to %v\n", c.Request.URL, req.URL)
	resp, err := client.Do(req)
	check(err)
	respData, err := io.ReadAll(resp.Body)

	for k, v := range resp.Header {
		c.Writer.Header()[k] = v
	}
	c.Status(resp.StatusCode)
	c.Writer.Write(respData)
	logMess := []byte(format_clf(c, "", fmt.Sprintf("%v", resp.StatusCode), fmt.Sprintf("%v", resp.ContentLength)) + "\n")
	accessLog.Write(logMess)
	accessLog.Flush()
}

// Quick and dirty HTML templating
func templateSet(template, before, after string) string {
	template = strings.Replace(template, before, after, -1)
	return template
}
