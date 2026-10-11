package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func shopRouter(t *testing.T) *gin.Engine {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatal(err)
		}
	})
	if err := os.Mkdir("userdata", 0700); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	configureRoutes(router, "/api/")
	return router
}

func shopRequest(router *gin.Engine, endpoint string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/"+endpoint, nil)
	request.Header.Set("authentigate-id", "buyer")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestCouponRoutesListAndDeletePersistedCoupons(t *testing.T) {
	router := shopRouter(t)
	response := shopRequest(router, "createCoupon?discount=0.7&duration=100&target=orange")
	var coupon Coupon
	if err := json.Unmarshal(response.Body.Bytes(), &coupon); err != nil || coupon.Name == "" {
		t.Fatalf("create coupon: status %d, body %s, error %v", response.Code, response.Body.String(), err)
	}
	for _, endpoint := range []string{"listCoupon", "listCoupons"} {
		response = shopRequest(router, endpoint)
		var coupons map[string]Coupon
		if err := json.Unmarshal(response.Body.Bytes(), &coupons); err != nil || len(coupons) != 1 || coupons[coupon.Name].Name != coupon.Name {
			t.Fatalf("%s returned %s; error %v", endpoint, response.Body.String(), err)
		}
	}
	response = shopRequest(router, "deleteCoupon?coupon="+url.QueryEscape(coupon.Name))
	if response.Code != http.StatusOK || len(LoadStore().Coupons) != 0 {
		t.Fatalf("coupon deletion not persisted: status %d, coupons %v", response.Code, LoadStore().Coupons)
	}
}

func TestPurchaseRejectsExpiredAndPreviouslyUsedDiscounts(t *testing.T) {
	router := shopRouter(t)
	for _, expired := range []bool{false, true} {
		coupon := doAddCoupon(60, 0.5, "orange")
		if expired {
			store := LoadStore()
			coupon.Expiry = time.Now().Add(-time.Second)
			store.Coupons[coupon.Name] = coupon
			SaveStore(store)
		}
		for purchaseNumber := 0; purchaseNumber < 2; purchaseNumber++ {
			if response := shopRequest(router, "addItem?item=orange&amount=1"); response.Code != http.StatusOK {
				t.Fatalf("add item: %s", response.Body.String())
			}
			response := shopRequest(router, "purchase?coupon="+coupon.Name)
			var order map[string]float64
			if err := json.Unmarshal(response.Body.Bytes(), &order); err != nil {
				t.Fatal(err)
			}
			expected := 4.0
			if !expired && purchaseNumber == 0 {
				expected = 2.0
			}
			if order["orange"] != expected {
				t.Fatalf("expired=%v purchase=%d: price %v; expected %v", expired, purchaseNumber, order["orange"], expected)
			}
		}
	}
}

func TestInvalidShopInputsDoNotChangeState(t *testing.T) {
	router := shopRouter(t)
	for _, endpoint := range []string{
		"addItem?item=orange&amount=-1", "addItem?item=unknown&amount=1", "addItem?item=orange&amount=bad",
		"createCoupon?discount=NaN", "createCoupon?discount=2", "createCoupon?discount=0.7&duration=-1",
		"createCoupon?discount=0.7&duration=bad", "createCoupon?discount=0.7&target=unknown",
	} {
		response := shopRequest(router, endpoint)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d, body %s", endpoint, response.Code, response.Body.String())
		}
	}
	if len(LoadUser("buyer").Basket) != 0 || len(LoadStore().Coupons) != 0 {
		t.Fatal("invalid inputs modified persisted state")
	}
}

func TestConcurrentAddsPreserveEveryItem(t *testing.T) {
	router := shopRouter(t)
	var requests sync.WaitGroup
	const count = 40
	for requestNumber := 0; requestNumber < count; requestNumber++ {
		requests.Add(1)
		go func() {
			defer requests.Done()
			response := shopRequest(router, "addItem?item=apple&amount=1")
			if response.Code != http.StatusOK {
				t.Errorf("concurrent add: status %d, body %s", response.Code, response.Body.String())
			}
		}()
	}
	requests.Wait()
	if got := LoadUser("buyer").Basket["apple"]; got != count {
		t.Fatalf("concurrent additions saved %d apples; expected %d", got, count)
	}
}

func TestCorruptedDataAndWriteFailuresAreReported(t *testing.T) {
	router := shopRouter(t)
	const invalid = `{"Basket":`
	if err := os.WriteFile("userdata/buyer.json", []byte(invalid), 0600); err != nil {
		t.Fatal(err)
	}
	response := shopRequest(router, "addItem?item=apple&amount=1")
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "could not decode user") {
		t.Fatalf("corrupt data response: status %d, body %s", response.Code, response.Body.String())
	}
	contents, err := os.ReadFile("userdata/buyer.json")
	if err != nil || string(contents) != invalid {
		t.Fatalf("corrupted data was overwritten: %q, error %v", contents, err)
	}
	if err := os.Mkdir("shop.json", 0700); err != nil {
		t.Fatal(err)
	}
	response = shopRequest(router, "reset")
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "could not replace shop.json") {
		t.Fatalf("failed write response: status %d, body %s", response.Code, response.Body.String())
	}
}

func TestUserIDCannotEscapeDataDirectory(t *testing.T) {
	router := shopRouter(t)
	request := httptest.NewRequest(http.MethodGet, "/api/reset", nil)
	request.Header.Set("authentigate-id", "../../outside")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "invalid user ID") {
		t.Fatalf("invalid user ID response: status %d, body %s", response.Code, response.Body.String())
	}
}

func TestLargeBasketRecalculatesWithoutQuantityLoop(t *testing.T) {
	count := int(^uint(0) >> 2)
	basket := recalculateBasket(map[string]int{"pear": count, "banana": count})
	if basket["fruit combo set"] != count/4 || basket["pear"] != count%4 {
		t.Fatalf("incorrect large basket %s: %v", strconv.Itoa(count), basket)
	}
}
