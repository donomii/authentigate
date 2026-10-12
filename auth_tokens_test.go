package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func useAuthTestStore(t *testing.T) {
	t.Helper()
	original := b
	store, err, closeStore := newAuthDB(filepath.Join(t.TempDir(), "auth_"))
	if err != nil {
		t.Fatal(err)
	}
	b = store
	t.Cleanup(func() {
		closeStore()
		b = original
	})
}

func createAuthTestUser(t *testing.T) *userData_t {
	t.Helper()
	user := &userData_t{Id: "auth-test-user", Token: "old-session-token", UserName: "alice", OtpSecret: "JBSWY3DPEHPK3PXP"}
	SaveUser(user)
	if err := b.SessionTokens.Set(user.Token, user.Id); err != nil {
		t.Fatal(err)
	}
	return user
}

func TestSessionTokenRotationRevokesPriorMappings(t *testing.T) {
	useAuthTestStore(t)
	user := createAuthTestUser(t)
	first := newToken(user.Id)
	second := newToken(user.Id)
	for _, token := range []string{first, second} {
		decoded, err := hex.DecodeString(token)
		if err != nil || len(decoded) != 32 {
			t.Fatalf("session token must contain 32 random bytes: bytes=%d error=%v", len(decoded), err)
		}
	}
	if first == second || first == user.Token || second == user.Token {
		t.Fatal("token rotation reused a token")
	}
	for _, oldToken := range []string{user.Token, first} {
		var id string
		found, err := b.SessionTokens.Get(oldToken, &id)
		if err != nil || found {
			t.Fatalf("obsolete mapping remains: found=%t error=%v", found, err)
		}
		if err := b.SessionTokens.Set(oldToken, user.Id); err != nil {
			t.Fatal(err)
		}
		if id := sessionTokenToId(oldToken); id != "" {
			t.Fatalf("obsolete index authenticated user %q", id)
		}
	}
	if id := sessionTokenToId(second); id != user.Id {
		t.Fatalf("current token returned user %q", id)
	}
	current := LoadUser(user.Id)
	if current.OtpSecret != user.OtpSecret || current.UserName != user.UserName {
		t.Fatal("rotation changed the authenticator enrollment or username")
	}
}

func TestConcurrentTokenRotationReturnsOneCurrentCookie(t *testing.T) {
	useAuthTestStore(t)
	user := createAuthTestUser(t)
	const requests = 8
	responses := make(chan *httptest.ResponseRecorder, requests)
	var workers sync.WaitGroup
	for range requests {
		workers.Add(1)
		go func() {
			defer workers.Done()
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/manage/"+user.Token+"/newToken", nil)
			newTokenHandler(&Context{Writer: response, Request: request}, user.Id, user.Token, nil, true)
			responses <- response
		}()
	}
	workers.Wait()
	close(responses)
	current := LoadUser(user.Id)
	successes := 0
	for response := range responses {
		if response.Code == http.StatusUnauthorized {
			continue
		}
		if response.Code != http.StatusOK {
			t.Fatalf("rotation status=%d", response.Code)
		}
		successes++
		cookies := response.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != "AuthentigateSessionToken" || cookies[0].Value != current.Token {
			t.Fatal("rotation did not return the current session cookie")
		}
		if !strings.Contains(response.Body.String(), current.Token) || strings.Contains(response.Body.String(), user.Token) {
			t.Fatal("rotation page and stored token disagree")
		}
		if !strings.Contains(response.Body.String(), "data:image/png;base64,") {
			t.Fatal("rotation page omitted the authenticator image")
		}
	}
	if successes != 1 {
		t.Fatalf("rotations succeeding with the same old token: got %d, want 1", successes)
	}
}

func TestManagementRechecksTokenAfterAuthentication(t *testing.T) {
	useAuthTestStore(t)
	user := createAuthTestUser(t)
	id := sessionTokenToId(user.Token)
	currentToken := newToken(user.Id)
	for _, handler := range []struct {
		name string
		call func(*Context, string, string, *Redirect, bool)
	}{{"token", tokenShowHandler}, {"updateUser", updateUserHandler}, {"newToken", newTokenHandler}} {
		t.Run(handler.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/manage/"+user.Token+"/"+handler.name, strings.NewReader("username=changed"))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			handler.call(&Context{Writer: response, Request: request}, id, user.Token, nil, false)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("stale management request status=%d", response.Code)
			}
		})
	}
	current := LoadUser(user.Id)
	if current.Token != currentToken || current.UserName != user.UserName {
		t.Fatal("stale management request changed the current user")
	}
}

func captureAuthLogs(t *testing.T) (*bytes.Buffer, *os.File) {
	t.Helper()
	var logs bytes.Buffer
	originalLog, originalStdout := log.Writer(), os.Stdout
	output, err := os.CreateTemp(t.TempDir(), "stdout-")
	if err != nil {
		t.Fatal(err)
	}
	log.SetOutput(&logs)
	os.Stdout = output
	t.Cleanup(func() {
		log.SetOutput(originalLog)
		os.Stdout = originalStdout
		output.Close()
	})
	return &logs, output
}

func TestAuthenticationLogsExcludeCredentials(t *testing.T) {
	useAuthTestStore(t)
	logs, stdout := captureAuthLogs(t)
	user := createAuthTestUser(t)
	newToken := newToken(user.Id)
	generateTOTPWithSecret(user.OtpSecret, user.UserName)
	request := httptest.NewRequest(http.MethodGet, "/manage/"+newToken+"/token?code=private-code&state=private-state", nil)
	request.SetPathValue("token", newToken)
	request.Pattern = "GET /manage/{token}/token"
	request.AddCookie(&http.Cookie{Name: "AuthentigateSessionToken", Value: newToken})
	request.Header.Set("Referer", "https://auth.example/secure/"+newToken+"/")
	request.Header.Set("Authorization", "Bearer private-authorization")
	response := httptest.NewRecorder()
	c := &Context{Writer: response, Request: request}
	displayLoginSuccessfulPage(c, user.Id, newToken)
	access := format_clf(c, user.Id, "200", "100")
	logRequestFailure(c, fmt.Sprintf("request %s failed: %s %s private-code private-state", request.URL, newToken, request.Header.Get("Authorization")))
	logOAuthFailure(c, errors.New(`oauth2: 401 Unauthorized Response: {"access_token":"private-provider-token","refresh_token":"private-refresh-token"}`))
	if _, err := stdout.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatal(err)
	}
	allLogs := logs.String() + string(output) + access
	for _, secret := range []string{user.Token, newToken, user.OtpSecret, "otpauth://", "private-code", "private-state", "private-authorization", "private-provider-token", "private-refresh-token"} {
		if strings.Contains(allLogs, secret) {
			t.Fatalf("logs contain credential %q", secret)
		}
	}
	if !strings.Contains(allLogs, "401 Unauthorized") || !strings.Contains(access, "/manage/{token}/token") {
		t.Fatal("logs lost the upstream failure status or registered route")
	}
}

func TestProxyLogsExcludeRequestCredentials(t *testing.T) {
	useAuthTestStore(t)
	user := createAuthTestUser(t)
	logs, stdout := captureAuthLogs(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authentigate-token") != user.Token {
			t.Error("proxy did not forward the session token")
		}
		io.Copy(io.Discard, r.Body)
		w.Write([]byte("upstream response"))
	}))
	defer upstream.Close()
	originalConfig, originalAccess := config, accessLog
	config = &Config{Port: 8000}
	var access bytes.Buffer
	accessLog = &access
	t.Cleanup(func() { config, accessLog = originalConfig, originalAccess })
	request := httptest.NewRequest(http.MethodPost, "/secure/"+user.Token+"/test/action?secret=private-query", strings.NewReader("password=private-password"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Authorization", "Bearer private-authorization")
	request.AddCookie(&http.Cookie{Name: "AuthentigateSessionToken", Value: user.Token})
	request.SetPathValue("api", "action")
	request.SetPathValue("token", user.Token)
	request.Pattern = "POST /secure/{token}/test/{api...}"
	response := httptest.NewRecorder()
	relay := &Redirect{Name: "test", To: upstream.URL}
	makeAuthedRelay(relayPostHandler, relay, nil)(response, request)
	if response.Body.String() != "upstream response" {
		t.Fatalf("proxy response=%q", response.Body.String())
	}
	if _, err := stdout.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatal(err)
	}
	allLogs := logs.String() + access.String() + string(output)
	for _, secret := range []string{user.Token, "private-query", "private-password", "private-authorization"} {
		if strings.Contains(allLogs, secret) {
			t.Fatalf("proxy logs contain credential %q", secret)
		}
	}
}

func TestURLFailuresKeepCauseWithoutCredentials(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/secure/private-session/api", nil)
	request.SetPathValue("token", "private-session")
	c := &Context{Request: request}
	err := &url.Error{Op: "Get", URL: "http://user:private-password@127.0.0.1/private-session?code=private-code", Err: errors.New("connection refused")}
	detail := requestLogDetail(c, err.Error())
	if strings.Contains(detail, "private-") || !strings.Contains(detail, "connection refused") {
		t.Fatalf("unsafe or incomplete diagnostic: %s", detail)
	}
}
