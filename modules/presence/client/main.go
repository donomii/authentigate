package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
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
func ip() (ipaddrs []string) {
	//Catch all errors and return
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
			debug.PrintStack()
		}
	}()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Println("Could not get local ip address because:", err)
		return nil
	}

	return interfaceIPv4Addresses(addrs)
}

func interfaceIPv4Addresses(addrs []net.Addr) []string {
	ipaddrs := []string{}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, _, err := net.ParseCIDR(address.String()); err == nil && !ipnet.IsLoopback() {
			if ipnet.To4() != nil {
				ipaddr1 := ipnet.String()
				ipaddrs = append(ipaddrs, ipaddr1)

			}
		}
	}
	if len(ipaddrs) > 0 {
		return ipaddrs
	}
	return ipaddrs
}

func getWrapper(token, hostname string, ip []string) {
	//Catch all errors and return
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
			debug.PrintStack()
		}
	}()
	//Call server using hostname and ip
	ips := strings.Join(ip, ",")
	callStr := "https://entirety.praeceptamachinae.com/secure/" + url.PathEscape(token) + "/presence/users?localip=" + url.QueryEscape(ips) + "&host=" + url.QueryEscape(hostname)
	client := &http.Client{Timeout: 10 * time.Second}
	thing, err := client.Get(callStr)
	if err == nil {
		thing.Body.Close()
		if thing.StatusCode != http.StatusOK {
			logPresenceStatus(hostname, thing.StatusCode)
		}
	} else {
		if requestError, ok := err.(*url.Error); ok {
			err = requestError.Err
		}
		logPresenceFailure(hostname, err)
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
