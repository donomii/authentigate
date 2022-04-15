package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

func hostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "Hostname not found"
	}
	return hostname
}

//Get local ip address
func ip() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "IP not found"
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "IP not found"
}

func getWrapper(hostname, ip string) {
	//Catch all errors and return
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()
	//Call server using hostname and ip
	thing, err := http.Get(fmt.Sprintf("https://entirety.praeceptamachinae.com/secure/2529910190816306683/presence/users?localip=%v&host=%v", ip, hostname))
	if err != nil {
		thing.Body.Close()
	}
}

func main() {
	hostname := hostname()
	ip := ip()
	for {
		getWrapper(hostname, ip)
		time.Sleep(time.Second * 10)
	}
}
