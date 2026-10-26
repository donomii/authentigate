package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
)

// Save user details
func SaveUser(user *userData_t) {
	b.mu.Lock()
	defer b.mu.Unlock()
	saveUserLocked(user)
}

func saveUserLocked(user *userData_t) {
	if err := b.Users.Set(user.Id, *user); err != nil {
		panic(fmt.Errorf("save user %q: %w", user.Id, err))
	}
	logUserSaved(user.Id)
}

// Load user data
func LoadUser(id string) *userData_t {
	b.mu.Lock()
	defer b.mu.Unlock()
	return loadUserLocked(id)
}

func loadUserLocked(id string) *userData_t {
	var user userData_t
	found, err := b.Users.Get(id, &user)
	if err != nil {
		panic(fmt.Errorf("load user %q: %w", id, err))
	}
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
	if err != nil {
		panic(fmt.Errorf("read session token mapping: %s", redactLogSecrets(err.Error(), sessionToken)))
	}
	if !found {
		return ""
	}
	user := loadUserLocked(id)
	if user == nil || user.Token != sessionToken {
		return ""
	}

	if id == "1" && !develop {
		panic("Invalid user id!  Id 1 is reserved for development")
	}
	return id
}

// Given a provider id (string is provider name + provider id), find the authentigate id
func foreignIdToId(fid string) string {
	var id string
	found, err := b.ForeignIDs.Get(fid, &id)
	if !found {
		return ""
	}
	check(err)

	return id
}

// Given an authentigate id, load the current session token
func idToSessionToken(id string) string {

	user := LoadUser(id)
	if user == nil {
		return ""
	}
	return string(user.Token)
}
