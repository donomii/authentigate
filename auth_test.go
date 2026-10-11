package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danilopolani/gocialite"
	"github.com/danilopolani/gocialite/structs"
	"github.com/donomii/gin"
	"github.com/xlzd/gotp"
	"golang.org/x/oauth2"
)

func authTestDatabase(t *testing.T) {
	t.Helper()
	oldDB, oldDevelop, oldBaseURL := b, develop, baseUrl
	oldAttempts := otpIPAttempts
	store, err, closeStore := newAuthDB(filepath.Join(t.TempDir(), "auth_"))
	if err != nil {
		t.Fatal(err)
	}
	b, develop, baseUrl = store, false, "https://auth.example/secure/"
	otpIPAttempts = make(map[string]otpAttemptWindow)
	t.Cleanup(func() {
		closeStore()
		b, develop, baseUrl = oldDB, oldDevelop, oldBaseURL
		otpIPAttempts = oldAttempts
	})
}

func authTestUser(t *testing.T, id, username string) *userData_t {
	t.Helper()
	user := &userData_t{Id: id, UserName: username, OtpSecret: randomOTPSecret()}
	SaveUser(user)
	newToken(id)
	if username != "" {
		if err := b.UserNames.Set(username, id); err != nil {
			t.Fatal(err)
		}
	}
	return LoadUser(id)
}

func authTestRouter() *gin.Engine {
	router := gin.New()
	router.Use(authRecovery)
	router.GET("/manage/:token/token", makeAuthedRelay(tokenShowHandler, nil, nil))
	router.GET("/manage/:token/newToken", makeAuthedRelay(newTokenHandler, nil, nil))
	router.POST("/manage/:token/newToken", makeAuthedRelay(newTokenHandler, nil, nil))
	router.POST("/manage/:token/updateUser", makeAuthedRelay(updateUserHandler, nil, nil))
	router.GET("/totplogin", totploginHandler)
	router.POST("/totplogin", totploginHandler)
	router.GET("/develop/auth/callback", developCallbackHandler)
	router.GET("/auth/:provider", redirectHandler)
	router.GET("/auth/:provider/callback", callbackHandler)
	return router
}

func authRequest(router http.Handler, method, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestTokenRotationRevokesOldTokenAndUpdatesCookie(t *testing.T) {
	authTestDatabase(t)
	user := authTestUser(t, "account-id", "alice")
	router := authTestRouter()
	path := "/manage/" + user.Token + "/newToken"
	page := authRequest(router, http.MethodGet, path, nil)
	if page.Code != http.StatusOK || LoadUser(user.Id).Token != user.Token {
		t.Fatalf("GET changed the token or failed: status=%d", page.Code)
	}
	response := authRequest(router, http.MethodPost, path, url.Values{"csrf": {user.Token}})
	updated := LoadUser(user.Id)
	if response.Code != http.StatusOK || updated.Token == user.Token {
		t.Fatalf("rotation failed: status=%d body=%s", response.Code, response.Body.String())
	}
	if sessionTokenToId(user.Token) != "" || sessionTokenToId(updated.Token) != user.Id {
		t.Fatal("rotation did not revoke the old token and retain the original user ID")
	}
	if b.Exists("sessionTokens", user.Token) || !b.Exists("users", user.Id) {
		t.Fatal("old token remains stored or user existence check failed")
	}
	if updated.OtpSecret != user.OtpSecret {
		t.Fatal("token rotation changed the enrolled authenticator secret")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(updated.Token)
	if err != nil || len(decoded) != 32 {
		t.Fatalf("expected a 256-bit session token: length=%d error=%v", len(decoded), err)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "__Host-AuthentigateSessionToken" || cookies[0].Value != updated.Token || cookies[0].Path != "/" || cookies[0].Domain != "" || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("rotation did not issue a protected replacement cookie: %+v", cookies)
	}
	if !strings.Contains(response.Body.String(), "data:image/png;base64,") || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("account page must embed the authenticator code and prevent caching")
	}
}

func TestConcurrentRotationsAndStaleManagementRequests(t *testing.T) {
	authTestDatabase(t)
	user := authTestUser(t, "account-id", "alice")
	router := authTestRouter()
	responses := make([]*httptest.ResponseRecorder, 2)
	var group sync.WaitGroup
	for index := range responses {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			responses[index] = authRequest(router, http.MethodPost, "/manage/"+user.Token+"/newToken", url.Values{"csrf": {user.Token}})
		}(index)
	}
	group.Wait()
	successes := 0
	for _, response := range responses {
		if response.Code == http.StatusOK {
			successes++
			cookie := response.Result().Cookies()[0]
			if sessionTokenToId(cookie.Value) != user.Id {
				t.Fatal("successful rotation issued an already-revoked token")
			}
		} else if response.Code != http.StatusUnauthorized {
			t.Fatalf("unexpected concurrent rotation response: status=%d", response.Code)
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one rotation to succeed: successes=%d", successes)
	}
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/manage/old/token", nil)
	tokenShowHandler(context, user.Id, user.Token, nil, false)
	if response.Code != http.StatusUnauthorized || strings.Contains(response.Body.String(), LoadUser(user.Id).Token) {
		t.Fatal("previously authorized old session disclosed the replacement token")
	}
}

func TestHTTPSIgnoresUnprefixedSessionCookie(t *testing.T) {
	authTestDatabase(t)
	user := authTestUser(t, "account-id", "alice")
	router := authTestRouter()
	response := authRequest(router, http.MethodGet, "/manage/c/token", nil, &http.Cookie{Name: "AuthentigateSessionToken", Value: user.Token})
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("HTTPS accepted an unprefixed session cookie: status=%d", response.Code)
	}
	baseUrl = "http://localhost:8000/secure/"
	response = authRequest(router, http.MethodGet, "/manage/c/token", nil, &http.Cookie{Name: "AuthentigateSessionToken", Value: user.Token})
	if response.Code != http.StatusOK {
		t.Fatalf("HTTP local session cookie failed: status=%d", response.Code)
	}
}

func TestManagementRejectsMissingCSRF(t *testing.T) {
	authTestDatabase(t)
	user := authTestUser(t, "account-id", "alice")
	router := authTestRouter()
	for _, action := range []string{"newToken", "updateUser"} {
		response := authRequest(router, http.MethodPost, "/manage/"+user.Token+"/"+action, url.Values{"username": {"attacker"}})
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s accepted a form without CSRF protection: status=%d", action, response.Code)
		}
	}
	updated := LoadUser(user.Id)
	if updated.Token != user.Token || updated.UserName != user.UserName {
		t.Fatal("rejected request changed account data")
	}
}

func TestCookieMutationsRejectOtherOrigins(t *testing.T) {
	authTestDatabase(t)
	user := authTestUser(t, "account-id", "alice")
	router := authTestRouter()
	for _, origin := range []string{"https://sibling.auth.example", "http://auth.example", "https://attacker.example", "null"} {
		request := httptest.NewRequest(http.MethodPost, "/manage/c/newToken", strings.NewReader(url.Values{"csrf": {user.Token}}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Origin", origin)
		request.AddCookie(&http.Cookie{Name: "__Host-AuthentigateSessionToken", Value: user.Token})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || LoadUser(user.Id).Token != user.Token {
			t.Fatalf("cookie mutation accepted origin %q: status=%d", origin, response.Code)
		}
	}
	baseUrl = "https://AUTH.EXAMPLE:443/secure/"
	request := httptest.NewRequest(http.MethodPost, "/manage/c/updateUser", strings.NewReader(url.Values{"csrf": {user.Token}, "username": {"alice"}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "https://auth.example")
	request.AddCookie(&http.Cookie{Name: "__Host-AuthentigateSessionToken", Value: user.Token})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("equivalent configured origin was rejected: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTOTPLoginWithoutSession(t *testing.T) {
	authTestDatabase(t)
	user := authTestUser(t, "account-id", "alice")
	router := authTestRouter()
	page := authRequest(router, http.MethodGet, "/totplogin", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "name=\"code\"") {
		t.Fatalf("anonymous authenticator login page unavailable: status=%d", page.Code)
	}
	response := authRequest(router, http.MethodPost, "/totplogin", url.Values{
		"username": {user.UserName}, "code": {gotp.NewDefaultTOTP(user.OtpSecret).Now()},
	})
	cookies := response.Result().Cookies()
	if response.Code != http.StatusOK || len(cookies) != 1 || cookies[0].Value != user.Token {
		t.Fatalf("valid authenticator login failed: status=%d body=%s", response.Code, response.Body.String())
	}
	invalid := authRequest(router, http.MethodPost, "/totplogin", url.Values{"username": {user.UserName}, "code": {"bad"}})
	if invalid.Code != http.StatusUnauthorized || len(invalid.Result().Cookies()) != 0 {
		t.Fatalf("invalid authenticator code accepted: status=%d", invalid.Code)
	}
}

func TestTOTPLoginRejectsAnotherOriginWithoutSession(t *testing.T) {
	authTestDatabase(t)
	user := authTestUser(t, "account-id", "alice")
	router := authTestRouter()
	form := url.Values{"username": {user.UserName}, "code": {gotp.NewDefaultTOTP(user.OtpSecret).Now()}}
	request := httptest.NewRequest(http.MethodPost, "/totplogin", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || len(response.Result().Cookies()) != 0 || LoadUser(user.Id).OTPLastStep != 0 {
		t.Fatalf("cross-origin authenticator login created a session or consumed a code: status=%d", response.Code)
	}
}

func TestTOTPAttemptLimitAndReplayPersistence(t *testing.T) {
	authTestDatabase(t)
	user := authTestUser(t, "account-id", "alice")
	const timestamp = int64(1800000000)
	for attempt := 0; attempt < otpAccountAttemptLimit; attempt++ {
		response := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(response)
		if verifyUserOTP(context, user, "invalid", timestamp) || response.Code != http.StatusUnauthorized {
			t.Fatalf("invalid code accepted on attempt %d: status=%d", attempt, response.Code)
		}
	}
	user = LoadUser(user.Id)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	if verifyUserOTP(context, user, gotp.NewDefaultTOTP(user.OtpSecret).At(timestamp), timestamp) || response.Code != http.StatusTooManyRequests {
		t.Fatalf("account attempt limit did not survive reload: status=%d", response.Code)
	}
	later := timestamp + otpAttemptWindowSeconds
	response = httptest.NewRecorder()
	context, _ = gin.CreateTestContext(response)
	code := gotp.NewDefaultTOTP(user.OtpSecret).At(later)
	if !verifyUserOTP(context, user, code, later) {
		t.Fatalf("account did not recover after attempt window: status=%d", response.Code)
	}
	user = LoadUser(user.Id)
	response = httptest.NewRecorder()
	context, _ = gin.CreateTestContext(response)
	if verifyUserOTP(context, user, code, later) || response.Code != http.StatusUnauthorized {
		t.Fatalf("accepted authenticator code could be replayed after reload: status=%d", response.Code)
	}
}

func TestOTPAddressAttemptsAreBounded(t *testing.T) {
	authTestDatabase(t)
	const timestamp = int64(1800000000)
	for attempt := 0; attempt < otpIPAttemptLimit; attempt++ {
		if !allowOTPAddress("192.0.2.1:5000", timestamp) {
			t.Fatalf("address rejected before attempt limit: attempt=%d", attempt)
		}
	}
	if allowOTPAddress("192.0.2.1:5001", timestamp) {
		t.Fatal("changing source port bypassed the address attempt limit")
	}
	if !allowOTPAddress("192.0.2.1:5001", timestamp+otpAttemptWindowSeconds) {
		t.Fatal("address remained blocked after its attempt window")
	}
	for index := 1; index < otpIPEntryLimit; index++ {
		if !allowOTPAddress(fmt.Sprintf("[2001:db8::%x]:5000", index), timestamp+otpAttemptWindowSeconds) {
			t.Fatalf("address rejected before entry limit: index=%d", index)
		}
	}
	if allowOTPAddress("192.0.2.2:5000", timestamp+otpAttemptWindowSeconds) || len(otpIPAttempts) != otpIPEntryLimit {
		t.Fatal("address attempt table exceeded its memory bound")
	}
}

func TestUsernameRenameAndHTMLSafety(t *testing.T) {
	authTestDatabase(t)
	user := authTestUser(t, "account-id", "alice")
	router := authTestRouter()
	username := `bob" autofocus onfocus="alert(1)`
	path := "/manage/" + user.Token + "/updateUser"
	form := url.Values{"csrf": {user.Token}, "username": {username}}
	for attempt := 0; attempt < 2; attempt++ {
		response := authRequest(router, http.MethodPost, path, form)
		if response.Code != http.StatusOK {
			t.Fatalf("rename or unchanged username rejected: status=%d body=%s", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), `value="bob" autofocus`) || !strings.Contains(response.Body.String(), "bob&#34;") {
			t.Fatal("account page did not escape the username as HTML")
		}
	}
	if b.Exists("userNames", "alice") || !b.Exists("userNames", username) {
		t.Fatal("username rename retained the old login alias or lost the new alias")
	}
}

func TestConcurrentUsernameClaimsHaveOneOwner(t *testing.T) {
	authTestDatabase(t)
	users := []*userData_t{authTestUser(t, "first", ""), authTestUser(t, "second", "")}
	router := authTestRouter()
	responses := make([]*httptest.ResponseRecorder, len(users))
	var group sync.WaitGroup
	for index, user := range users {
		group.Add(1)
		go func(index int, user *userData_t) {
			defer group.Done()
			responses[index] = authRequest(router, http.MethodPost, "/manage/"+user.Token+"/updateUser", url.Values{"csrf": {user.Token}, "username": {"shared"}})
		}(index, user)
	}
	group.Wait()
	successes, conflicts := 0, 0
	for _, response := range responses {
		switch response.Code {
		case http.StatusOK:
			successes++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("unexpected claim response: status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected one owner and one conflict: successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestDevelopmentIdentityCannotAuthenticateOutsideDevelopment(t *testing.T) {
	authTestDatabase(t)
	develop = true
	router := authTestRouter()
	for attempt := 0; attempt < 2; attempt++ {
		response := authRequest(router, http.MethodGet, "/develop/auth/callback", nil)
		if response.Code != http.StatusOK || LoadUser("1") == nil {
			t.Fatalf("development login did not use reserved identity: status=%d", response.Code)
		}
	}
	user := LoadUser("1")
	legacy := authTestUser(t, "old-random-development-id", "")
	legacy.ForeignIDs = []string{"1"}
	SaveUser(legacy)
	develop = false
	if sessionTokenToId(user.Token) != "" || sessionTokenToId(legacy.Token) != "" {
		t.Fatal("development account authenticated with development mode disabled")
	}
}

func TestSessionRequiresCurrentUserToken(t *testing.T) {
	authTestDatabase(t)
	user := authTestUser(t, "account-id", "")
	if err := b.SessionTokens.Set("stale-token", user.Id); err != nil {
		t.Fatal(err)
	}
	if err := b.SessionTokens.Set("orphan-token", "nonexistent-user"); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "stale-token", "orphan-token"} {
		if sessionTokenToId(token) != "" {
			t.Fatalf("invalid token authenticated: %q", token)
		}
	}
}

func TestSaveUserPropagatesStoreError(t *testing.T) {
	authTestDatabase(t)
	defer func() {
		if recover() == nil {
			t.Fatal("SaveUser silently accepted an invalid empty store key")
		}
	}()
	SaveUser(&userData_t{})
}

func TestOAuthBindsBrowserAndProviderAndConsumesState(t *testing.T) {
	authTestDatabase(t)
	var tokenMu sync.Mutex
	codeUsed := false
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/token":
			tokenMu.Lock()
			defer tokenMu.Unlock()
			if codeUsed {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			codeUsed = true
			w.Write([]byte(`{"access_token":"local-access","token_type":"Bearer"}`))
		case "/user":
			if request.Header.Get("Authorization") != "Bearer local-access" {
				t.Error("user endpoint did not receive the exchanged OAuth access token")
			}
			w.Write([]byte(`{"id":"local-account"}`))
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(provider.Close)
	gocialite.RegisterNewDriver("localtest", nil, func(client *http.Client, user *structs.User) {
		if user.ID != "local-account" {
			t.Errorf("unexpected provider account ID: %q", user.ID)
		}
	}, oauth2.Endpoint{AuthURL: provider.URL + "/authorize", TokenURL: provider.URL + "/token"},
		map[string]string{"endpoint": provider.URL, "userEndpoint": "/user"}, map[string]string{"id": "ID"})
	oldSecrets := providerSecrets
	providerSecrets = map[string]map[string]string{"localtest": {
		"clientID": "local-client", "clientSecret": "local-secret", "redirectURL": "https://auth.example/auth/localtest/callback",
	}}
	t.Cleanup(func() { providerSecrets = oldSecrets })
	router := authTestRouter()
	start := authRequest(router, http.MethodGet, "/auth/localtest", nil)
	if start.Code != http.StatusFound || len(start.Result().Cookies()) != 1 {
		t.Fatalf("OAuth login did not start: status=%d body=%s", start.Code, start.Body.String())
	}
	location, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := location.Query().Get("state")
	callback := "/auth/localtest/callback?" + url.Values{"state": {state}, "code": {"local-code"}}.Encode()
	wrongBrowser := authRequest(router, http.MethodGet, callback, nil)
	if wrongBrowser.Code != http.StatusBadRequest {
		t.Fatalf("OAuth callback accepted another browser: status=%d", wrongBrowser.Code)
	}
	stateCookie := start.Result().Cookies()[0]
	if stateCookie.Name != "__Host-AuthentigateOAuthState" || stateCookie.Path != "/" || stateCookie.Domain != "" {
		t.Fatalf("OAuth state cookie lacks HTTPS host binding: %+v", stateCookie)
	}
	wrongProvider := authRequest(router, http.MethodGet, strings.Replace(callback, "localtest", "github", 1), nil, stateCookie)
	if wrongProvider.Code != http.StatusBadRequest {
		t.Fatalf("OAuth callback accepted another provider: status=%d", wrongProvider.Code)
	}
	response := authRequest(router, http.MethodGet, callback, nil, stateCookie)
	if response.Code != http.StatusOK || foreignIdToId("localtestlocal-account") == "" {
		t.Fatalf("valid OAuth callback failed: status=%d body=%s", response.Code, response.Body.String())
	}
	replay := authRequest(router, http.MethodGet, callback, nil, stateCookie)
	if replay.Code != http.StatusBadGateway {
		t.Fatalf("OAuth state was reused: status=%d", replay.Code)
	}
	start = authRequest(router, http.MethodGet, "/auth/localtest", nil)
	stateCookie = start.Result().Cookies()[0]
	payload, err := json.Marshal(oauthLogin{Provider: "localtest", Expires: time.Now().Add(-time.Minute).Unix(), Nonce: "expired-login"})
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	stateCookie.Value = encoded + "." + signOAuthState(encoded, "local-secret")
	expired := authRequest(router, http.MethodGet, "/auth/localtest/callback?"+url.Values{"state": {stateCookie.Value}, "code": {"local-code"}}.Encode(), nil, stateCookie)
	if expired.Code != http.StatusBadRequest {
		t.Fatalf("expired OAuth state accepted: status=%d", expired.Code)
	}
}
