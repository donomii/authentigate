package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/danilopolani/gocialite"
)

type oauthLogin struct {
	Provider string `json:"provider"`
	Expires  int64  `json:"expires"`
	Nonce    string `json:"nonce"`
}

func authRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		defer func() {
			if failure := recover(); failure != nil {
				logAuthFailure(&Context{Writer: w, Request: request}, fmt.Sprint(failure))
				http.Error(w, "Authentication request failed", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, request)
	})
}

func developmentUserDisabled(user *userData_t) bool {
	if develop {
		return false
	}
	if user.Id == "1" {
		return true
	}
	for _, foreignID := range user.ForeignIDs {
		if foreignID == "1" {
			return true
		}
	}
	return false
}

func randomSessionToken() string {
	data := make([]byte, 32)
	_, err := rand.Read(data)
	check(err)
	return base64.RawURLEncoding.EncodeToString(data)
}

func randomOTPSecret() string {
	data := make([]byte, 20)
	_, err := rand.Read(data)
	check(err)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(data)
}

func randomUserID() string {
	data := make([]byte, 16)
	_, err := rand.Read(data)
	check(err)
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", data[:4], data[4:6], data[6:8], data[8:10], data[10:])
}

func setAuthCookie(c *Context, name, value, path string, maxAge int) {
	secure := secureAuthRequest(c)
	if secure {
		path = "/"
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: authCookieName(c, name), Value: value, Path: path, MaxAge: maxAge,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

func secureAuthRequest(c *Context) bool {
	base, err := url.Parse(baseUrl)
	return c.Request.TLS != nil || (err == nil && strings.EqualFold(base.Scheme, "https"))
}

func authCookieName(c *Context, name string) string {
	if secureAuthRequest(c) {
		return "__Host-" + name
	}
	return name
}

func sameAuthOrigin(expected, received *url.URL) bool {
	return strings.EqualFold(received.Scheme, expected.Scheme) && strings.EqualFold(received.Hostname(), expected.Hostname()) && originPort(received) == originPort(expected)
}

func originPort(origin *url.URL) string {
	if port := origin.Port(); port != "" {
		return port
	}
	if strings.EqualFold(origin.Scheme, "https") {
		return "443"
	}
	return "80"
}

func validCookieOrigin(c *Context) bool {
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	origin := c.Request.Header.Get("Origin")
	if origin == "" {
		if c.Request.Header.Get("Sec-Fetch-Site") != "cross-site" {
			return true
		}
	} else {
		expected, err := url.Parse(baseUrl)
		received, originErr := url.Parse(origin)
		if err == nil && originErr == nil && sameAuthOrigin(expected, received) && received.Path == "" && received.RawQuery == "" && received.Fragment == "" && received.User == nil {
			return true
		}
	}
	c.String(http.StatusForbidden, "Cookie-authenticated account changes require the configured Authentigate origin")
	return false
}

func managementUser(c *Context, id, token string) *userData_t {
	user := LoadUser(id)
	if user == nil || subtle.ConstantTimeCompare([]byte(user.Token), []byte(token)) != 1 || developmentUserDisabled(user) {
		c.String(http.StatusUnauthorized, "This session token is no longer current; log in again")
		return nil
	}
	return user
}

func validManagementForm(c *Context, token string) bool {
	if c.Request.Method != http.MethodPost {
		c.String(http.StatusMethodNotAllowed, "Account changes require a POST form submission")
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	if err := c.Request.ParseForm(); err != nil {
		c.String(http.StatusBadRequest, "Unable to read account management form")
		return false
	}
	csrf := c.Request.PostForm.Get("csrf")
	if token == "" || subtle.ConstantTimeCompare([]byte(csrf), []byte(token)) != 1 {
		c.String(http.StatusForbidden, "Account management form has an invalid security token; reload the account page")
		return false
	}
	return true
}

func displayUserPage(c *Context, user *userData_t, message string) {
	qr := ""
	if user.UserName != "" {
		qr = "data:image/png;base64," + base64.StdEncoding.EncodeToString(generateTOTPWithSecret(user.OtpSecret, user.UserName))
	}
	displayPage(c, user.Token, "files/showToken.html", nil, map[string]string{
		"QRCODE": qr, "USERNAME": user.UserName, "ERROR": message,
		"UPDATEUSERURL": "/manage/" + user.Token + "/updateUser",
		"NEWTOKENURL":   "/manage/" + user.Token + "/newToken",
	})
}

func renderAuthPage(template, token string, subs map[string]string) string {
	values := map[string]string{
		"TOKEN": token, "BASE": baseUrl, "SECUREURL": makeSecureZoneUrl(token, ""),
		"QRCODE": "", "USERNAME": "", "ERROR": "", "UPDATEUSERURL": "", "NEWTOKENURL": "", "DEVELOPER": "", "SIGNUP_FORM": "", "QRCODE_BLOCK": "", "MESSAGE": "",
	}
	for key, value := range subs {
		values[key] = value
	}
	replacements := make([]string, 0, len(values)*2)
	for _, key := range []string{"SIGNUP_FORM", "QRCODE_BLOCK", "MESSAGE", "UPDATEUSERURL", "NEWTOKENURL", "SECUREURL", "DEVELOPER", "USERNAME", "QRCODE", "ERROR", "TOKEN", "BASE"} {
		value := values[key]
		if key != "DEVELOPER" && key != "SIGNUP_FORM" && key != "QRCODE_BLOCK" && key != "MESSAGE" {
			value = html.EscapeString(value)
		}
		replacements = append(replacements, key, value)
	}
	return strings.NewReplacer(replacements...).Replace(template)
}

func loadProviderData(provider string) (map[string]string, error) {
	providerData, found := providerSecrets[provider]
	if !found || providerData["clientID"] == "" || providerData["clientSecret"] == "" || providerData["redirectURL"] == "" {
		return nil, fmt.Errorf("provider %q needs clientID, clientSecret, and redirectURL in provider_secrets.json", provider)
	}
	return providerData, nil
}

func loadProviderSecrets(filename string) error {
	configData, err := os.ReadFile(filename)
	if err != nil {
		if develop && os.IsNotExist(err) {
			providerSecrets = map[string]map[string]string{}
			return nil
		}
		return fmt.Errorf("read OAuth provider configuration %s: %w", filename, err)
	}
	var secrets map[string]map[string]string
	if err := json.Unmarshal(configData, &secrets); err != nil {
		return fmt.Errorf("parse OAuth provider configuration %s: %w", filename, err)
	}
	providerSecrets = secrets
	for provider := range secrets {
		if _, _, err := newOAuthFlow(provider); err != nil {
			return fmt.Errorf("validate OAuth provider %s in %s: %w", provider, filename, err)
		}
	}
	return nil
}

func newOAuthFlow(provider string) (*gocialite.Gocial, *url.URL, error) {
	providerData, err := loadProviderData(provider)
	if err != nil {
		return nil, nil, err
	}
	flow := new(gocialite.Gocial)
	authURL, err := flow.Driver(provider).Redirect(providerData["clientID"], providerData["clientSecret"], providerData["redirectURL"])
	if err != nil {
		return nil, nil, err
	}
	parsedURL, err := url.Parse(authURL)
	if err != nil {
		return nil, nil, err
	}
	return flow, parsedURL, nil
}

func signOAuthState(payload, secret string) string {
	signature := hmac.New(sha256.New, []byte(secret))
	signature.Write([]byte("authentigate-oauth-state:" + payload))
	return base64.RawURLEncoding.EncodeToString(signature.Sum(nil))
}

func beginOAuth(c *Context, provider string) (string, error) {
	_, authURL, err := newOAuthFlow(provider)
	if err != nil {
		return "", err
	}
	login := oauthLogin{Provider: provider, Expires: time.Now().Add(10 * time.Minute).Unix(), Nonce: randomSessionToken()}
	payloadBytes, err := json.Marshal(login)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	state := payload + "." + signOAuthState(payload, providerSecrets[provider]["clientSecret"])
	query := authURL.Query()
	query.Set("state", state)
	authURL.RawQuery = query.Encode()
	setAuthCookie(c, "AuthentigateOAuthState", state, "/", 600)
	return authURL.String(), nil
}

func consumeOAuth(c *Context, provider, state string) (*gocialite.Gocial, string, error) {
	cookieState, err := c.Cookie(authCookieName(c, "AuthentigateOAuthState"))
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookieState), []byte(state)) != 1 {
		return nil, "", fmt.Errorf("browser does not match the login request")
	}
	providerData, err := loadProviderData(provider)
	if err != nil {
		return nil, "", err
	}
	parts := strings.Split(state, ".")
	if len(parts) != 2 || !hmac.Equal([]byte(parts[1]), []byte(signOAuthState(parts[0], providerData["clientSecret"]))) {
		return nil, "", fmt.Errorf("invalid login request signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, "", fmt.Errorf("invalid login request payload")
	}
	var login oauthLogin
	if json.Unmarshal(payload, &login) != nil || time.Now().Unix() >= login.Expires || login.Provider != provider || login.Nonce == "" {
		return nil, "", fmt.Errorf("login request is expired or belongs to another provider")
	}
	flow, authURL, err := newOAuthFlow(provider)
	if err != nil {
		return nil, "", err
	}
	setAuthCookie(c, "AuthentigateOAuthState", "", "/", -1)
	// The signed browser state is checked above; the library's fresh state only satisfies its local equality check.
	return flow, authURL.Query().Get("state"), nil
}
