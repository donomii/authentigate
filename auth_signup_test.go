package main

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xlzd/gotp"
)

func authTestSignup(t *testing.T, username string) (http.Handler, pendingUser) {
	t.Helper()
	authTestDatabase(t)
	project, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	if err := os.Symlink(filepath.Join(project, "files"), "files"); err != nil {
		t.Fatal(err)
	}
	router := authTestRouter()
	page := authRequest(router, http.MethodGet, "/totpsignup", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `action="/totpsignup"`) {
		t.Fatalf("signup form unavailable: status=%d body=%s", page.Code, page.Body.String())
	}
	signup := authRequest(router, http.MethodPost, "/totpsignup", url.Values{"username": {username}})
	if signup.Code != http.StatusOK || !strings.Contains(signup.Body.String(), "data:image/png;base64,") || len(signup.Result().Cookies()) != 0 {
		t.Fatalf("signup for %q did not display enrollment without a session: status=%d", username, signup.Code)
	}
	pending, found := loadPending().get(username)
	if !found || pending.Approved || pending.Username != username || pending.OtpSecret == "" {
		t.Fatalf("signup for %q did not persist an unapproved enrollment: found=%v approved=%v", username, found, pending.Approved)
	}
	return router, pending
}

func TestTOTPSignupRequiresApproval(t *testing.T) {
	const username = "waiting-for-approval"
	router, pending := authTestSignup(t, username)
	login := authRequest(router, http.MethodPost, "/totplogin", url.Values{
		"username": {username}, "code": {gotp.NewDefaultTOTP(pending.OtpSecret).Now()},
	})
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), "Account pending approval") || len(login.Result().Cookies()) != 0 {
		t.Fatalf("unapproved account %q did not remain pending: status=%d body=%s", username, login.Code, login.Body.String())
	}
	if b.Exists("userNames", username) {
		t.Fatalf("unapproved account %q received a full user record", username)
	}
	duplicate := authRequest(router, http.MethodPost, "/totpsignup", url.Values{"username": {username}})
	retained, found := loadPending().get(username)
	if duplicate.Code != http.StatusOK || !strings.Contains(duplicate.Body.String(), "already registered and awaiting approval") || strings.Contains(duplicate.Body.String(), "data:image/png;base64,") || !found || retained.OtpSecret != pending.OtpSecret {
		t.Fatalf("duplicate signup changed or exposed enrollment for %q: status=%d found=%v", username, duplicate.Code, found)
	}
}

func TestApprovedTOTPSignupCreatesSessionAndRejectsReplay(t *testing.T) {
	const username = "approved-enrollment"
	router, pending := authTestSignup(t, username)
	approval := loadPending()
	pending.Approved = true
	approval.Users[username] = pending
	if err := approval.save(); err != nil {
		t.Fatal(err)
	}
	invalid := authRequest(router, http.MethodPost, "/totplogin", url.Values{"username": {username}, "code": {"invalid"}})
	if invalid.Code != http.StatusUnauthorized || len(invalid.Result().Cookies()) != 0 || !loadPending().exists(username) || b.Exists("userNames", username) {
		t.Fatalf("invalid code promoted approved enrollment %q: status=%d", username, invalid.Code)
	}
	form := url.Values{"username": {username}, "code": {gotp.NewDefaultTOTP(pending.OtpSecret).Now()}}
	login := authRequest(router, http.MethodPost, "/totplogin", form)
	cookies := login.Result().Cookies()
	if login.Code != http.StatusOK || len(cookies) != 1 {
		t.Fatalf("approved enrollment %q did not create a session: status=%d body=%s", username, login.Code, login.Body.String())
	}
	cookie := cookies[0]
	if cookie.Name != "__Host-AuthentigateSessionToken" || !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("approved enrollment %q received an unprotected session cookie", username)
	}
	id := sessionTokenToId(cookie.Value)
	user := LoadUser(id)
	if id == "" || user == nil || user.UserName != username || user.OtpSecret != pending.OtpSecret || user.OTPLastStep == 0 || loadPending().exists(username) {
		t.Fatalf("approved enrollment %q was not completely promoted: userID=%q", username, id)
	}
	replay := authRequest(router, http.MethodPost, "/totplogin", form)
	if replay.Code != http.StatusUnauthorized || len(replay.Result().Cookies()) != 0 || sessionTokenToId(cookie.Value) != id {
		t.Fatalf("enrollment code for %q was reusable or invalidated the issued session: status=%d", username, replay.Code)
	}
}
