package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
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
	var config Config
	raw, err := ioutil.ReadFile(file)
	if err != nil {
		log.Printf("Unable to read from file %a: %v", file, err)
		panic(fmt.Sprintf("Unable to read from file %a: %v", file, err))
	}

	json.Unmarshal(raw, &config)
	return &config
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