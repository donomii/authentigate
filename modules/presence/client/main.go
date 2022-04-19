package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/donomii/goof"
)

var token = ""

func hostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "Hostname not found"
	}
	return hostname
}

//Get local ip address
func ip() (ipaddr string) {
	//Catch all errors and return
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
			debug.PrintStack()
		}
	}()
	ipaddr = "IP.address.not.found"
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Println("Could not get local ip address because:", err)
		return "IP.address.not.found"
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ok {
				if ipnet.IP.To4() != nil {
					ipaddr = ipnet.IP.String()
					return ipaddr
				}
			} else {
				log.Println("Could not get local ip address")
			}
		}
	}
	return "IP.address.not.found"
}

func getWrapper(token, hostname, ip string) {
	//Catch all errors and return
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
			debug.PrintStack()
		}
	}()
	//Call server using hostname and ip
	callStr := fmt.Sprintf("https://entirety.praeceptamachinae.com/secure/"+token+"/presence/users?localip=%v&host=%v", ip, hostname)
	log.Println(callStr)
	thing, err := http.Get(callStr)
	if err == nil {
		thing.Body.Close()
	} else {
		fmt.Println("Error:", err)
	}
}

func main() {
	hostname := hostname()
	ip := ip()
	for {
		exeDir := goof.ExecutablePath()
		token_b, _ := ioutil.ReadFile(exeDir + "/presence.token")
		token = string(token_b)
		if token == "" {
			token_b, _ := ioutil.ReadFile(goof.HomeDirectory() + "/.presence.token")
			token = string(token_b)
		}
		if token == "" {
			panic("Could not find token in " + exeDir + "/presence.token or " + goof.HomeDirectory() + "/.presence.token")
		}
		token = strings.TrimSuffix(token, "\n") //Fucking unix newlines bullshit
		getWrapper(token, hostname, ip)
		time.Sleep(time.Second * 10)
	}
}
