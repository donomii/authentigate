package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
)

const pendingFile = "totp_pending.json"

type pendingUser struct {
	Username  string `json:"username"`
	OtpSecret string `json:"otp_secret"`
	Approved  bool   `json:"approved"`
}

type pendingStore struct {
	mu    sync.Mutex
	Users map[string]pendingUser `json:"users"`
}

func loadPending() *pendingStore {
	ps := &pendingStore{Users: make(map[string]pendingUser)}
	data, err := os.ReadFile(pendingFile)
	if err != nil {
		if os.IsNotExist(err) {
			return ps
		}
		log.Printf("Error reading %s: %v", pendingFile, err)
		return ps
	}
	if err := json.Unmarshal(data, ps); err != nil {
		log.Printf("Error parsing %s: %v", pendingFile, err)
	}
	return ps
}

func (ps *pendingStore) save() error {
	data, err := json.MarshalIndent(ps, "", "\t")
	if err != nil {
		return fmt.Errorf("marshalling pending store: %w", err)
	}
	return os.WriteFile(pendingFile, data, 0600)
}

func (ps *pendingStore) add(username, otpSecret string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.Users[username] = pendingUser{
		Username:  username,
		OtpSecret: otpSecret,
		Approved:  false,
	}
	return ps.save()
}

func (ps *pendingStore) get(username string) (pendingUser, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	u, ok := ps.Users[username]
	return u, ok
}

func (ps *pendingStore) exists(username string) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	_, ok := ps.Users[username]
	return ok
}

func (ps *pendingStore) remove(username string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	delete(ps.Users, username)
	return ps.save()
}
