package main

import (
	"bufio"
	"bytes"

	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"

	"github.com/gin-gonic/autotls"
	"os"
	"strings"
	"time"

	_ "github.com/philippgille/gokv"
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
	logMess := format_clf(&c, "", "???", "???" + "\n")
	fmt.Println(logMess)
	switch r.Method {
	case "GET":
		log.Println("GET method")
		for _, v := range config.Redirects {
			//log.Println("Comparing ", c.Request.Host, "and", v.Host)
			if strings.HasSuffix(c.Request.Host, v.Host) {
				//log.Println("Relaying for "+v.Host)
				relayGetHandler(&c, &v)
			}

		}
	case "POST":
		log.Println("POST method")
		for _, v := range config.Redirects {
			//log.Println("Comparing ", c.Request.Host, "and", v.Host)
			if strings.HasSuffix(c.Request.Host, v.Host) {
				//log.Println("Relaying for "+v.Host)
				relayPostHandler(&c, &v)
			}

		}

	case "PUT":
		log.Println("PUT method")
		for _, v := range config.Redirects {
			//log.Println("Comparing ", c.Request.Host, "and", v.Host)
			if strings.HasSuffix(c.Request.Host, v.Host) {
				//log.Println("Relaying for "+v.Host)
				relayPutHandler(&c, &v)
			}

		}

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
	config = LoadConfig("config.json")
	if config == nil {
		fmt.Printf("Failed to read config file, exiting!\n")
		os.Exit(1)
	}

	var err error
	var f *os.File
	f, err = os.Create("accessLog")
	check(err)
	accessLog = bufio.NewWriterSize(f, 999999) //Golang!
	flag.BoolVar(&develop, "develop", false, "Allow log in with no password")
	flag.Parse()

	r = rand.New(rand.NewSource(time.Now().UnixNano()))
	mux := MyMux{}

	if develop {
		http.ListenAndServe(":3000", mux)
	} else {
		log.Fatal(autotls.Run(mux, config.HostNames...))
	}
}

// Redirect to default microservice, using PUT
func relayPutHandler(c *MyContext, relay *Redirect) {

	bodyData, _ := ioutil.ReadAll(c.Request.Body)
	client := &http.Client{}
	path := c.Request.URL.Path

	req, err := http.NewRequest("PUT", relay.To+path, nil)

	req.Header.Add("X-Forwarded-For", c.Request.RemoteAddr)
	req.Header.Add("X-Real-IP", c.RemoteIP())

	for k, v := range c.Request.Header {
		req.Header[k] = v
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
	log.Printf("redirect PUT  %v\n", req.URL)
	accessLog.Write([]byte(format_clf(c, "", fmt.Sprintf("%v", resp.StatusCode), fmt.Sprintf("%v", resp.ContentLength)) + "\n"))
	accessLog.Flush()
}

// Redirect to default microservice, using POST
func relayPostHandler(c *MyContext, relay *Redirect) {

	path := c.Request.URL.Path
	bodyData, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		panic(err)
	}
	client := &http.Client{}

	req, err := http.NewRequest("POST", relay.To+path, nil)

	req.Header.Add("X-Forwarded-For", c.Request.RemoteAddr)
	req.Header.Add("X-Real-IP", c.Request.RemoteAddr)

	CopyHeaders := relay.CopyHeaders
	log.Printf("Forwarding headers %v\n", CopyHeaders)
	log.Printf("Request %+V\n", c)
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

	log.Printf("Sending Request %+V\n", req)
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
		log.Printf("redirect POST %v\n", req.URL)
		accessLog.Write([]byte(format_clf(
			c,
			"",
			fmt.Sprintf("%v", resp.StatusCode),
			fmt.Sprintf("%v", resp.ContentLength)) + "\n"))
	} else {
		log.Printf("redirect POST %v\n", req.URL)
		log.Printf("redirect failed %+V\n", resp)

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
	respData, err := ioutil.ReadAll(resp.Body)

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
