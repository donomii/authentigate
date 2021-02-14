package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	//"github.com/gin-gonic/autotls"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var safe bool = false

type User struct {
	Name        string
	Basket      map[string]int
	UsedCoupons map[string]int
	Orders      []map[string]float64
}

type Coupon struct {
	Name     string
	Expiry   time.Time
	Target   string
	Discount float64
}

type Shop struct {
	Prices  map[string]float64
	Coupons map[string]Coupon
}

func makeAuthed(handlerFunc func(*gin.Context, string, string)) func(c *gin.Context) {
	return func(c *gin.Context) {
		id := c.Request.Header.Get("authentigate-id")
		fmt.Printf("Request headers: %+v\n", c.Request.Header)
		if id == "" {
			id = "demoUser"
		}
		baseUrl := c.Request.Header.Get("authentigate-base-url")
		log.Printf("Got real user id: '%v'", id)
		handlerFunc(c, id, baseUrl)
	}
}

func doAddCoupon(duration int, discount float64, target string) Coupon {
	details := Coupon{}
	details.Expiry = time.Now().UTC().Add(time.Second * time.Duration(duration))
	details.Discount = discount
	coupon := uuid.New().String()
	details.Name = coupon
	details.Target = target

	shop := LoadStore()
	shop.Coupons[coupon] = details
	SaveStore(shop)
	return details
}

func createCoupon(c *gin.Context, id string, token string) {
	duration_s := c.Query("duration")
	discount, err := strconv.ParseFloat(c.Query("discount"), 64)
	target := c.Query("target")
	if target == "" {
		target = "orange"
	}
	duration, err := strconv.Atoi(duration_s)
	if duration_s == "" || err != nil {
		duration = 10
	}

	details := doAddCoupon(duration, discount, target)
	json.NewEncoder(c.Writer).Encode(details)
}

func listCoupons(c *gin.Context, id string, token string) {
	json.NewEncoder(c.Writer).Encode(LoadStore().Coupons)
}

func listItems(c *gin.Context, id string, token string) {
	json.NewEncoder(c.Writer).Encode(LoadStore().Prices)
}

func deleteCoupon(c *gin.Context, id string, token string) {
	coupon, _ := c.GetQuery("coupon")
	delete(LoadStore().Coupons, coupon)
}

func addItem(c *gin.Context, id string, token string) {
	item := c.Query("item")
	amountText := c.Query("amount")
	amount, err := strconv.Atoi(amountText)
	if err != nil {
		panic(err)
	}
	user := LoadUser(id)
	basket := user.Basket
	basket[item] = basket[item] + amount
	SaveUser(id, user)
}

func reset(c *gin.Context, id string, token string) {
	blankUser := User{Name: id, Basket: map[string]int{}, UsedCoupons: map[string]int{}, Orders: []map[string]float64{}}
	SaveUser(id, &blankUser)
	blankShop := Shop{Prices: map[string]float64{"apple": 1.0, "banana": 2.0, "pear": 3.0, "orange": 4.0, "fruit combo set": 18.0 * 0.7}, Coupons: map[string]Coupon{}}
	SaveStore(&blankShop)
}

func recalculateBasket(oldBasket map[string]int) map[string]int {
	basket := make(map[string]int)

	for key, value := range oldBasket {
		basket[key] = value
	}

	for basket["pear"] > 3 && basket["banana"] > 1 {
		basket["fruit combo set"] = basket["fruit combo set"] + 1
		basket["pear"] = basket["pear"] - 4
		basket["banana"] = basket["banana"] - 2
	}
	return basket
}

func basket(c *gin.Context, id string, token string) {
	basket := LoadUser(id).Basket
	newBasket := recalculateBasket(basket)
	json.NewEncoder(c.Writer).Encode(newBasket)
}

func calculateOrder(inputBasket map[string]int, coupon string, user *User) map[string]float64 {
	price := LoadStore().Prices
	basket := recalculateBasket(inputBasket)
	coupon_details, coupon_exists := LoadStore().Coupons[coupon]
	fmt.Printf("Coupon found: %+v\n", coupon_details)

	order := map[string]float64{}

	for item, count := range basket {
		order[item] = float64(count) * price[item]
	}
	if basket["apple"] > 6 {
		order["apple"] = 0.9 * float64(order["apple"])
	}
	if coupon_exists && user.UsedCoupons[coupon] != 1 {
		order[coupon_details.Target] = coupon_details.Discount * float64(order[coupon_details.Target])
	}
	return order
}

func checkout(c *gin.Context, id string, token string) {
	coupon, _ := c.GetQuery("coupon")
	user := LoadUser(id)
	basket := recalculateBasket(user.Basket)
	order := calculateOrder(basket, coupon, user)
	json.NewEncoder(c.Writer).Encode(order)
}

func purchase(c *gin.Context, id string, token string) {
	coupon, _ := c.GetQuery("coupon")

	user := LoadUser(id)
	basket := recalculateBasket(user.Basket)
	order := calculateOrder(basket, coupon, user)
	user.Orders = append(user.Orders, order)
	user.Basket = map[string]int{}
	SaveUser(id, user)
	json.NewEncoder(c.Writer).Encode(order)
}

func main() {
	os.Mkdir("userdata", 0700)
	router := gin.Default()
	coupon := doAddCoupon(20000, 0.7, "orange")
	fmt.Printf("Your discount coupon for oranges is: %v\n", coupon.Name)
	serveDemo(router, "/shoppr/")
	router.Run("127.0.0.1:98")
}

func serveDemo(router *gin.Engine, prefix string) {

	router.GET(prefix+"basket", makeAuthed(basket))
	router.GET(prefix+"checkout", makeAuthed(checkout))
	router.GET(prefix+"purchase", makeAuthed(purchase))
	router.GET(prefix+"addItem", makeAuthed(addItem))
	router.GET(prefix+"createCoupon", makeAuthed(createCoupon))
	router.GET(prefix+"listCoupon", makeAuthed(createCoupon))
	router.GET(prefix+"deleteCoupon", makeAuthed(createCoupon))
	router.GET(prefix+"reset", makeAuthed(reset))
	router.GET(prefix+"listItems", makeAuthed(listItems))
}
