package main

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	_ "github.com/swaggo/swag/example/celler/httputil"
)

// swagger:route GET /shoppr/api/v1/createCoupon coupon createCoupon
// Create a new coupon for given duration, discount and target item.
// responses:
//   200: Coupon

// A coupon
// swagger:response Coupon
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

	//json.NewEncoder(c.Writer).Encode(details)
	c.JSON(http.StatusOK, details)
}

// swagger:route GET /shoppr/api/v1/listCoupons coupon listCoupons
// List all coupons.
// responses:
//   200: []Coupon
// List of coupons
// swagger:response []Coupon
func listCoupons(c *gin.Context, id string, token string) {
	json.NewEncoder(c.Writer).Encode(LoadStore().Coupons)
}

// swagger:route GET /shoppr/api/v1/listItems shop listItems
// List all coupons.
// responses:
//   200: []Coupon

// List of coupons
// swagger:response []Coupon
func listItems(c *gin.Context, id string, token string) {
	json.NewEncoder(c.Writer).Encode(LoadStore().Prices)
}

// swagger:route GET /shoppr/api/v1/deleteCoupon coupon deleteCoupon
// Delete a coupon.
// responses:
//   200:

// no response
// swagger:response
func deleteCoupon(c *gin.Context, id string, token string) {
	coupon, _ := c.GetQuery("coupon")
	delete(LoadStore().Coupons, coupon)
}

// swagger:route GET /shoppr/api/v1/addItem basket addItem
// Add item to basket.
// responses:
//   200:

// noresponse
// swagger:response
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

// swagger:route GET /shoppr/api/v1/basket basket basket
// Show contents of basket.
// responses:
//   200: map[string]int

// map of items to amount
// swagger:response map[string]int
func basket(c *gin.Context, id string, token string) {
	basket := LoadUser(id).Basket
	newBasket := recalculateBasket(basket)

	json.NewEncoder(c.Writer).Encode(newBasket)
}

// swagger:route GET /shoppr/api/v1/reset reset reset
// Reset all data for testing.
// responses:
//   200:

// noresponse
// swagger:response
func reset(c *gin.Context, id string, token string) {
	blankUser := User{Name: id, Basket: map[string]int{}, UsedCoupons: map[string]int{}, Orders: []map[string]float64{}}
	SaveUser(id, &blankUser)
	blankShop := Shop{Prices: map[string]float64{"apple": 1.0, "banana": 2.0, "pear": 3.0, "orange": 4.0, "fruit combo set": 18.0 * 0.7}, Coupons: map[string]Coupon{}}
	SaveStore(&blankShop)
}

// swagger:route GET /shoppr/api/v1/checkout basket checkout
// Calculate checkout details.
// responses:
//   200: map[string]float64

// A map of item to total price
// swagger:response map[string]float64
func checkout(c *gin.Context, id string, token string) {

	user := LoadUser(id)
	basket := recalculateBasket(user.Basket)
	order := calculateOrder(basket, "", user)

	json.NewEncoder(c.Writer).Encode(order)
}

// swagger:route GET /shoppr/api/v1/purchase basket purchase
// Purchase basket.
// responses:
//   200: map[string]float64

// A map of item to total price
// swagger:response map[string]float64
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
