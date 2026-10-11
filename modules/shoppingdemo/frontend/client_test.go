package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func shopFiles(t *testing.T, files map[string]string) *gin.Engine {
	t.Helper()
	directory := t.TempDir()
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(http.FileServer(http.Dir(directory)))
	t.Cleanup(server.Close)
	previous := shopService
	shopService = server.URL + "/"
	t.Cleanup(func() { shopService = previous })
	router := gin.New()
	serveDemo(router, "/")
	return router
}

func TestPurchaseDoesNotConfirmFailedServiceRequest(t *testing.T) {
	router := shopFiles(t, nil)
	response := httptest.NewRecorder()
	request := authorizedForm("/purchase", "buyer", "gateway-token")
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "Demo order recorded") {
		t.Fatalf("failed service request reported success: status %d, body %s", response.Code, response.Body.String())
	}
}

func TestInvalidServiceJSONReturnsGatewayError(t *testing.T) {
	router := shopFiles(t, map[string]string{"basket": `{"orange":`})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/basket", nil))
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "invalid JSON") {
		t.Fatalf("invalid JSON response: status %d, body %s", response.Code, response.Body.String())
	}
}

func authorizedForm(endpoint, id, token string) *http.Request {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	context.Request.Header.Set("authentigate-id", id)
	context.Request.Header.Set("authentigate-token", token)
	form := url.Values{"csrf": {formToken(context)}}
	request := httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("authentigate-id", id)
	request.Header.Set("authentigate-token", token)
	return request
}

func TestStateChangesRequirePostAndUserBoundForm(t *testing.T) {
	router := shopFiles(t, nil)
	for _, endpoint := range []string{"/purchase", "/addItem", "/coupon"} {
		for _, request := range []*http.Request{
			httptest.NewRequest(http.MethodGet, endpoint, nil),
			httptest.NewRequest(http.MethodPost, endpoint, nil),
			authorizedForm(endpoint, "other-user", "other-token"),
		} {
			request.Header.Set("authentigate-id", "buyer")
			request.Header.Set("authentigate-token", "gateway-token")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			expected := http.StatusForbidden
			if request.Method == http.MethodGet {
				expected = http.StatusNotFound
			}
			if response.Code != expected {
				t.Fatalf("%s %s: status %d; expected %d", request.Method, endpoint, response.Code, expected)
			}
		}
	}
}

func TestCheckoutPostsDemoOrderWithoutCardFields(t *testing.T) {
	router := shopFiles(t, map[string]string{"basket": `{"orange":1}`, "checkout": `{"orange":4}`, "purchase": `{"orange":4}`})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/checkout", nil))
	page := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(page, `method="post" action="purchase"`) || !strings.Contains(page, "without collecting payment") {
		t.Fatalf("checkout does not explain demo order behavior: status %d, body %s", response.Code, page)
	}
	for _, field := range []string{"ccname", "ccnumber", "ccexpiry"} {
		if strings.Contains(page, field) {
			t.Fatalf("checkout still collects unused payment field %s", field)
		}
	}
	token := regexp.MustCompile(`name="csrf" value="([a-f0-9]+)"`).FindStringSubmatch(page)
	if len(token) != 2 {
		t.Fatal("checkout form does not contain its authorization token")
	}
	request := httptest.NewRequest(http.MethodPost, "/purchase", strings.NewReader(url.Values{"csrf": {token[1]}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Demo order recorded") || !strings.Contains(response.Body.String(), "No payment was collected") {
		t.Fatalf("valid demo order form failed: status %d, body %s", response.Code, response.Body.String())
	}
}

func TestBasketEscapesItemHTML(t *testing.T) {
	router := shopFiles(t, map[string]string{"basket": `{"<script>alert(1)</script>":1}`})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/basket", nil))
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "<script>") || !strings.Contains(response.Body.String(), "&lt;script&gt;") {
		t.Fatalf("basket item not escaped: status %d, body %s", response.Code, response.Body.String())
	}
}
