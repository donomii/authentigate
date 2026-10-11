package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/autotls"
	_ "github.com/philippgille/gokv"
)

//The url of the top of the authenticated part of your website

var develop = false
var accessLog *log.Logger
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
	host := requestHostname(r.Host)
	var route *Redirect
	for index := range config.Redirects {
		candidate := &config.Redirects[index]
		routeHost := requestHostname(candidate.Host)
		matches := host == routeHost || (net.ParseIP(routeHost) == nil && strings.HasSuffix(host, "."+routeHost))
		if routeHost != "" && matches && (route == nil || len(routeHost) > len(requestHostname(route.Host))) {
			route = candidate
		}
	}
	if route == nil {
		logMissingRoute(&c)
		http.Error(w, "404 - No route configured for this host", http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		relayGetHandler(&c, route)
	case http.MethodPost:
		relayPostHandler(&c, route)
	case http.MethodPut:
		relayPutHandler(&c, route)
	default:
		relayHandler(&c, route)
	}
}

func requestHostname(host string) string {
	if hostname, _, err := net.SplitHostPort(host); err == nil {
		host = hostname
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
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
	config = LoadConfig("config.json")
	if config == nil {
		fmt.Printf("Failed to read config file, exiting!\n")
		os.Exit(1)
	}

	var err error
	var f *os.File
	f, err = os.OpenFile(config.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	check(err)
	accessLog = log.New(f, "", 0)
	flag.BoolVar(&develop, "develop", false, "Serve HTTP on port 3000 instead of automatic HTTPS")
	flag.Parse()

	r = rand.New(rand.NewSource(time.Now().UnixNano()))
	mux := MyMux{}

	if develop {
		check(http.ListenAndServe(":3000", mux))
	} else {
		log.Fatal(autotls.Run(mux, config.HostNames...))
	}
}

// Redirect to default microservice, using PUT
func relayPutHandler(c *MyContext, relay *Redirect) {
	relayHandler(c, relay)
}

// Redirect to default microservice, using POST
func relayPostHandler(c *MyContext, relay *Redirect) {
	relayHandler(c, relay)
}

// Redirect to default microservice, using GET
func relayGetHandler(c *MyContext, relay *Redirect) {
	relayHandler(c, relay)
}

func relayHandler(c *MyContext, relay *Redirect) {
	target, err := parseTarget(relay.To)
	if err != nil {
		logInvalidTarget(relay.To, err)
		http.Error(c.Writer, "Invalid upstream URL", http.StatusBadGateway)
		return
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.SetXForwarded()
			clientIP, _, splitError := net.SplitHostPort(request.In.RemoteAddr)
			if splitError != nil {
				clientIP = request.In.RemoteAddr
			}
			request.Out.Header.Set("X-Real-IP", clientIP)
			port := "80"
			if request.In.TLS != nil {
				port = "443"
			}
			if _, hostPort, err := net.SplitHostPort(request.In.Host); err == nil {
				port = hostPort
			}
			request.Out.Header.Set("X-Forwarded-Port", port)
		},
		ErrorHandler: func(writer http.ResponseWriter, request *http.Request, err error) {
			logUpstreamError(relay.To, request.Method, err)
			http.Error(writer, "Upstream request failed", http.StatusBadGateway)
		},
	}
	response := &relayResponseWriter{ResponseWriter: c.Writer, status: http.StatusOK}
	proxy.ServeHTTP(response, &c.Request)
	logAccess(c, response.status, response.size)
}

func parseTarget(address string) (*url.URL, error) {
	target, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("parse upstream URL %q: %w", address, err)
	}
	if target.Hostname() == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return nil, fmt.Errorf("upstream URL %q must use http or https and include a hostname", address)
	}
	return target, nil
}

type relayResponseWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (writer *relayResponseWriter) WriteHeader(status int) {
	if status >= 200 || status == http.StatusSwitchingProtocols {
		writer.status = status
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *relayResponseWriter) Write(payload []byte) (int, error) {
	written, err := writer.ResponseWriter.Write(payload)
	writer.size += written
	return written, err
}

func (writer *relayResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

// Quick and dirty HTML templating
func templateSet(template, before, after string) string {
	template = strings.Replace(template, before, after, -1)
	return template
}
