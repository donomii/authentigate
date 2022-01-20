package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-resty/resty"
)

var base string = "http://localhost:98/shoppr/api/v1/"

type Coupon struct {
	Name     string
	Expiry   time.Time
	Target   string
	Discount float64
}

func rest(base, endpoint string, data interface{}) *resty.Response {
	client := resty.New()
	resp, err := client.R().Get(base + endpoint)
	if err != nil {
		fmt.Println(err)
		return nil
	}
	if data != nil {
		json.Unmarshal(resp.Body(), data)
	}
	return resp
}

func testAddItem() {

	var basket map[string]int
	rest(base, "reset", nil)
	rest(base, "addItem?item=apple&amount=3", nil)
	rest(base, "addItem?item=pear&amount=5", nil)
	rest(base, "basket", &basket)

	if basket["apple"] == 3 && basket["pear"] == 5 {
		fmt.Println("AddItem pass!")
	} else {
		fmt.Println("AddItem fail!")
		fmt.Printf("%+v\n", basket)
	}
}

func testMakeCoupon() {
	var coupon Coupon
	rest(base, "reset", nil)
	rest(base, "createCoupon?duration=100000&discount=0.7&target=orange", &coupon)
	expiry := time.Now().UTC().Add(time.Second * time.Duration(100000))

	timeDiff := expiry.Sub(coupon.Expiry)

	if coupon.Discount == 0.7 && coupon.Target == "orange" && timeDiff.Seconds() < 2 {
		fmt.Println("makeCoupon pass!")
	} else {
		fmt.Println("makeCoupon fail!")
		fmt.Printf("%+v\n", coupon)
		fmt.Printf("Time difference: %v, our expiry: %v\n", timeDiff.Seconds(), expiry)
	}
}

func testBasketSet() {
	var basket map[string]int
	rest(base, "reset", nil)
	rest(base, "addItem?item=pear&amount=9", nil)
	rest(base, "addItem?item=banana&amount=5", nil)
	resp:=rest(base, "basket", &basket)

	if basket["fruit combo set"] == 2 && basket["pear"] == 1 && basket["banana"] == 1 {
		fmt.Println("Basket set pass!")
	} else {
		fmt.Println("Basket set fail!")
		dumpResponse(resp)
	}
}

func testCheckout() {
	var order map[string]float64
	rest(base, "shoppr/reset", nil)
	rest(base, "addItem?item=orange&amount=3", nil)
	resp:=rest(base, "checkout", &order)

	if order["orange"] == 12.0 {
		fmt.Println("Checkout pass!")
	} else {
		fmt.Println("Checkout fail!")
		dumpResponse(resp)
	}
}

func testAppleDiscount() {
	var order map[string]float64
	rest(base, "reset", nil)
	rest(base, "addItem?item=apple&amount=7", nil)
	resp:=rest(base, "checkout", &order)

	if order["apple"] == 7.0*0.9 {
		fmt.Println("Apple discount pass!")
	} else {
		fmt.Println("Apple discount fail!")
		dumpResponse(resp)
	}
}

func testBasket() {
	var basket map[string]int
	rest(base, "reset", nil)
	resp:=rest(base, "basket", &basket)

	if resp != nil {
		fmt.Println("Basket pass!")
	} else {
		fmt.Println("Basket fail!")
		dumpResponse(resp)
	}
}

func testSetDiscount() {
	var order map[string]float64
	rest(base, "reset", nil)
	rest(base, "addItem?item=pear&amount=9", nil)
	rest(base, "addItem?item=banana&amount=5", nil)
	resp:=rest(base, "checkout", &order)

	if order["fruit combo set"] == 2.0*18.0*0.7 && order["pear"] == 3.0 && order["banana"] == 2.0 {
		fmt.Println("Set discount pass!")
	} else {
		fmt.Println("Set discount fail!")
		dumpResponse(resp)
	}
}

func dumpResponse(resp *resty.Response) {
	fmt.Printf("\nResponse Status Code: %v", resp.StatusCode())
	fmt.Printf("\nResponse Status: %v", resp.Status())
	fmt.Printf("\nResponse Body: %v", resp)
	fmt.Printf("\nResponse Time: %v", resp.Time())
	fmt.Printf("\nResponse Received At: %v", resp.ReceivedAt())

	var basket map[string]int
	json.Unmarshal(resp.Body(), &basket)
	fmt.Printf("Basket: %v\n", basket)
}
func main() {
	testBasket()
	testAddItem()
	testCheckout()
	testAppleDiscount()
	testBasketSet()
	testSetDiscount()
	testMakeCoupon()

}
