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
func ip() (ipaddrs []string) {
	//Catch all errors and return
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
			debug.PrintStack()
		}
	}()
	ipaddrs = []string{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Println("Could not get local ip address because:", err)
		//return "IP.address.error"
		//Fall thorugh to shell script
	}

	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ok {
				if ipnet.IP.To4() != nil {
					ipaddr1 := ipnet.IP.String()
					ipaddrs = append(ipaddrs, ipaddr1)
				}
			} else {
				log.Println("Could not get local ip address")
			}
		}
	}
	if len(ipaddrs) > 0 {
		return ipaddrs
	}
	ipaddr_str := goof.Shell("/usr/sbin/ifconfig | /usr/bin/grep 'inet' | /usr/bin/grep -v 127.0.0.1 | /usr/bin/awk '{print $2}'")
	for _, ipaddr1 := range strings.Split(ipaddr_str, "\n") {
		if ipaddr1 != "" {
			ipaddrs = append(ipaddrs, ipaddr1)
		}
	}
	if len(ipaddrs) > 0 {
		return ipaddrs
	}

	return []string{"IP.address.not.found"}
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
	callStr := fmt.Sprintf("https://entirety.praeceptamachinae.com/secure/"+token+"/presence/users?localip=%v&host=%v", ips, hostname)
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
