package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"strings"
	"time"
)

//Save user details
func SaveUser(user *userData_t) {
	b.Users.Set(user.Id, *user)
	log.Printf("Saved user %+v\n", user)
}
// Load user data
func LoadUser(id string) *userData_t {
	var user userData_t
	found, err := b.Users.Get(id, &user)
	if err != nil || !found {
		return nil
	}

	return &user
}

func SaveConfig(config *Config) {
	payload, err := json.MarshalIndent(config, "", "	")
	if err != nil {
		panic("Could not marshall config")
	}
	ioutil.WriteFile(fmt.Sprintf("config.json"), payload, 0600)
}

//Load user data
func LoadConfig(file string) *Config {
	config := Config{LogFile: "accessLog", Secure: true, MaxBodyBytes: defaultMaxBodyBytes, UpstreamTimeoutSeconds: defaultUpstreamTimeoutSeconds}
	raw, err := ioutil.ReadFile(file)
	if err != nil {
		panic(fmt.Sprintf("Unable to read config file %q: %v", file, err))
	}

	if err := json.Unmarshal(raw, &config); err != nil {
		panic(fmt.Sprintf("Unable to decode config file %q: %v", file, err))
	}
	if err := validateConfig(&config); err != nil {
		panic(fmt.Sprintf("Invalid config file %q: %v", file, err))
	}
	return &config
}

func validateConfig(config *Config) error {
	base, err := url.Parse(config.BaseUrl)
	if err != nil || base.Hostname() == "" || (base.Scheme != "http" && base.Scheme != "https") || !strings.HasSuffix(base.Path, "/") || base.RawQuery != "" || base.Fragment != "" || base.User != nil {
		return fmt.Errorf("BaseUrl must be an absolute HTTP or HTTPS URL ending in / without credentials, query, or fragment; received %q", config.BaseUrl)
	}
	if config.Port < 1 || config.Port > 65535 {
		return fmt.Errorf("Port must be between 1 and 65535; received %d", config.Port)
	}
	if config.Secure && len(config.HostNames) == 0 {
		return fmt.Errorf("HostNames must contain at least one certificate hostname when Secure is true")
	}
	if config.LogFile == "" {
		return fmt.Errorf("LogFile must name the access log file")
	}
	if config.MaxBodyBytes < 1 || config.MaxBodyBytes == 1<<63-1 {
		return fmt.Errorf("MaxBodyBytes must be positive and below %d; received %d", int64(1<<63-1), config.MaxBodyBytes)
	}
	if config.UpstreamTimeoutSeconds < 1 || int64(config.UpstreamTimeoutSeconds) > int64((1<<63-1)/time.Second) {
		return fmt.Errorf("UpstreamTimeoutSeconds must be a positive duration; received %d", config.UpstreamTimeoutSeconds)
	}
	for _, relay := range config.Redirects {
		target, err := url.Parse(relay.To)
		if err != nil || target.Hostname() == "" || (target.Scheme != "http" && target.Scheme != "https") || target.Fragment != "" {
			return fmt.Errorf("route %q To must be an absolute HTTP or HTTPS URL without a fragment; received %q", relay.Name, relay.To)
		}
		if relay.Name == "" || !strings.HasPrefix(relay.From, "/") {
			return fmt.Errorf("each route needs Name and an absolute From path; received name=%q from=%q", relay.Name, relay.From)
		}
		if relay.Tipe != "GET" && relay.Tipe != "POST" && relay.Tipe != "PUT" {
			return fmt.Errorf("route %q Tipe must be GET, POST, or PUT; received %q", relay.Name, relay.Tipe)
		}
	}
	return nil
}



// Given a session token, find the authentigate id
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



// Given a provider id (string is provider name + provider id), find the authentigate id
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

// Given an authentigate id, load the current session token
func idToSessionToken(id string) string {

	user := LoadUser(id)
	if user == nil {
		return ""
	}
	log.Printf("Token %v found for id %v", user.Token, id)
	return string(user.Token)
}
