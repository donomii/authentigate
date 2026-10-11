package main

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/donomii/gin"
	"github.com/xlzd/gotp"
)

type otpAttemptWindow struct {
	started int64
	count   int
}

var otpAttemptsMu sync.Mutex
var otpIPAttempts = make(map[string]otpAttemptWindow)

const otpAttemptWindowSeconds = 300
const otpAccountAttemptLimit = 5
const otpIPAttemptLimit = 20
const otpIPEntryLimit = 4096

func allowOTPAddress(address string, now int64) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	otpAttemptsMu.Lock()
	defer otpAttemptsMu.Unlock()
	for key, window := range otpIPAttempts {
		if now-window.started >= otpAttemptWindowSeconds {
			delete(otpIPAttempts, key)
		}
	}
	window, found := otpIPAttempts[host]
	if !found {
		if len(otpIPAttempts) >= otpIPEntryLimit {
			return false
		}
		window.started = now
	}
	if window.count >= otpIPAttemptLimit {
		return false
	}
	window.count++
	otpIPAttempts[host] = window
	return true
}

func verifyUserOTP(c *gin.Context, user *userData_t, code string, now int64) bool {
	if now-user.OTPWindowStart >= otpAttemptWindowSeconds {
		user.OTPWindowStart = now
		user.OTPFailures = 0
	}
	if user.OTPFailures >= otpAccountAttemptLimit {
		remaining := otpAttemptWindowSeconds - (now - user.OTPWindowStart)
		c.Header("Retry-After", strconv.FormatInt(remaining, 10))
		c.String(http.StatusTooManyRequests, "Too many authenticator attempts for this account; retry after %d seconds", remaining)
		return false
	}
	currentCode := gotp.NewDefaultTOTP(user.OtpSecret).At(now)
	if user.OTPLastStep >= now/30 || subtle.ConstantTimeCompare([]byte(code), []byte(currentCode)) != 1 {
		user.OTPFailures++
		SaveUser(user)
		c.String(http.StatusUnauthorized, "Invalid username or authenticator code, or code already used")
		return false
	}
	user.OTPLastStep = now / 30
	user.OTPFailures = 0
	SaveUser(user)
	return true
}

func allowOTPLogin(c *gin.Context) bool {
	if !allowOTPAddress(c.Request.RemoteAddr, time.Now().Unix()) {
		c.Header("Retry-After", "300")
		c.String(http.StatusTooManyRequests, "Too many authenticator attempts from this address; retry after five minutes")
		return false
	}
	return true
}
