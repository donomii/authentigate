package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
)

// Save user details
func SaveUser(user *userData_t) {
	check(b.Users.Set(user.Id, *user))
}

// Load user data
func LoadUser(id string) *userData_t {
	var user userData_t
	found, err := b.Users.Get(id, &user)
	check(err)
	if !found {
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

// Load user data
func LoadConfig(file string) *Config {
	var config Config
	raw, err := ioutil.ReadFile(file)
	if err != nil {
		log.Printf("Unable to read from file %s: %v", file, err)
		panic(fmt.Sprintf("Unable to read from file %s: %v", file, err))
	}

	json.Unmarshal(raw, &config)
	return &config
}

// Given a session token, find the authentigate id
func sessionTokenToId(sessionToken string) string {
	if sessionToken == "" {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var id string
	found, err := b.SessionTokens.Get(sessionToken, &id)
	check(err)
	if !found {
		return ""
	}
	user := LoadUser(id)
	if user == nil || user.Id != id || subtle.ConstantTimeCompare([]byte(user.Token), []byte(sessionToken)) != 1 || developmentUserDisabled(user) {
		return ""
	}
	return id
}

// Given a provider id (string is provider name + provider id), find the authentigate id
func foreignIdToId(fid string) string {
	var id string
	found, err := b.ForeignIDs.Get(fid, &id)
	check(err)
	if !found {
		return ""
	}
	return string(id)
}

// Given an authentigate id, load the current session token
func idToSessionToken(id string) string {

	user := LoadUser(id)
	if user == nil {
		return ""
	}
	return string(user.Token)
}
