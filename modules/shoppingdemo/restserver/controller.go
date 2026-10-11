package main

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
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
	if err != nil || math.IsNaN(discount) || math.IsInf(discount, 0) || discount < 0 || discount > 1 {
		NewError(c, http.StatusBadRequest, "Discount must be a number between 0 and 1")
		return
	}
	target := c.Query("target")
	if target == "" {
		target = "orange"
	}
	duration, err := strconv.Atoi(duration_s)
	if duration_s == "" {
		duration = 10
	} else if err != nil || duration <= 0 || int64(duration) > math.MaxInt64/int64(time.Second) {
		NewError(c, http.StatusBadRequest, "Duration must be a positive number of seconds within the supported range")
		return
	}
	if _, exists := LoadStore().Prices[target]; !exists {
		NewError(c, http.StatusBadRequest, "Unknown coupon target: "+target)
		return
	}
	details := doAddCoupon(duration, discount, target)

	//json.NewEncoder(c.Writer).Encode(details)
	c.JSON(http.StatusOK, details)
}

// swagger:route GET /shoppr/api/v1/updateCoupon coupon updateCoupon
// Update an existing coupon with given duration, discount and target item.
// responses:
//   200: Coupon

// A coupon
// swagger:response Coupon
func updateCoupon(c *gin.Context, id string, token string) {
	createCoupon(c, id, token)
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
	store := LoadStore()
	delete(store.Coupons, coupon)
	SaveStore(store)
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
	if err != nil || amount <= 0 {
		NewError(c, http.StatusBadRequest, "Item amount must be a positive integer: "+amountText)
		return
	}
	if _, exists := LoadStore().Prices[item]; !exists {
		NewError(c, http.StatusBadRequest, "Unknown item: "+item)
		return
	}
	user := LoadUser(id)
	basket := user.Basket
	if amount > int(^uint(0)>>1)-basket[item] {
		NewError(c, http.StatusBadRequest, "Item amount exceeds the supported basket quantity for "+item)
		return
	}
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

	c.JSON(http.StatusOK, newBasket)
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

func checkPayment(ccname, ccnumber, ccexpiry string) {}

// swagger:route GET /shoppr/api/v1/purchase basket purchase
// Purchase basket.
// responses:
//   200: map[string]float64

// A map of item to total price
// swagger:response map[string]float64
func purchase(c *gin.Context, id string, token string) {
	coupon, _ := c.GetQuery("coupon")
	ccname, _ := c.GetQuery("ccname")
	ccnumber, _ := c.GetQuery("ccnumber")
	ccexpiry, _ := c.GetQuery("ccexpiry")

	user := LoadUser(id)
	basket := recalculateBasket(user.Basket)
	order := calculateOrder(basket, coupon, user)
	//Valid payment here
	checkPayment(ccname, ccnumber, ccexpiry)
	if details, exists := LoadStore().Coupons[coupon]; exists && details.Expiry.After(time.Now()) && basket[details.Target] > 0 {
		user.UsedCoupons[coupon] = 1
	}
	user.Orders = append(user.Orders, order)
	user.Basket = map[string]int{}
	SaveUser(id, user)

	json.NewEncoder(c.Writer).Encode(order)
}
