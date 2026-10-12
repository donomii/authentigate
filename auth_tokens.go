package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/xlzd/gotp"
)

func newSessionToken() string {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		panic(fmt.Errorf("generate session token: %w", err))
	}
	return hex.EncodeToString(token[:])
}

func rotateUserTokenLocked(user *userData_t) string {
	oldToken := user.Token
	sessionToken := newSessionToken()
	user.Token = sessionToken
	if user.OtpSecret == "" {
		user.OtpSecret = gotp.RandomSecret(16)
		if user.OtpSecret == "" {
			panic("generate authenticator secret: no random bytes available")
		}
	}
	if user.UserName != "" {
		user.QRcode = generateTOTPWithSecret(user.OtpSecret, user.UserName)
	}
	if err := b.SessionTokens.Set(sessionToken, user.Id); err != nil {
		panic(fmt.Errorf("save session token mapping for user %q: %s", user.Id, redactLogSecrets(err.Error(), sessionToken)))
	}
	saveUserLocked(user)
	// The user record is authoritative even if deleting an obsolete index fails.
	if oldToken != "" {
		if err := b.SessionTokens.Delete(oldToken); err != nil {
			logTokenCleanupFailure(user.Id, oldToken, sessionToken, err)
		}
	}
	logTokenRotated(user.Id)
	return sessionToken
}

func currentManagementUserLocked(c *Context, id, token string) *userData_t {
	user := loadUserLocked(id)
	if user == nil || user.Token != token {
		http.Error(c.Writer, "Session is no longer valid", http.StatusUnauthorized)
		return nil
	}
	return user
}

func displayUserTokenPage(c *Context, user *userData_t, cookies map[string]string) {
	displayPage(c, user.Token, "files/showToken.html", cookies, map[string]string{
		"QRCODE":        "data:image/png;base64," + base64.StdEncoding.EncodeToString(user.QRcode),
		"UPDATEUSERURL": "/manage/" + user.Token + "/updateUser",
		"USERNAME":      user.UserName,
		"ERROR":         "",
	})
}
