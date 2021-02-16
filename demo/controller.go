package main

import (
	"encoding/json"
	"strconv"

	"github.com/gin-gonic/gin"
)

// @title Shop Demo
// @version 1.0
// @description Example fruit store
// @termsOfService https://www.gnu.org/licenses/agpl-3.0.en.html

// @contact.name Jeremy Price
// @contact.url http://praeceptamachinae.com
// @contact.email jeremy@praeceptamachinae.com

// @license.name Affero GPL
// @license.url https://www.gnu.org/licenses/agpl-3.0.en.html

// @host https://entirety.praeceptamachinae.com/
// @BasePath /api/v1

//

// createCoupon godoc
// @Summary Create a new coupon
// @Description Create a new coupon
// @Tags coupon
// @Accept  json
// @Produce  json
// @Param duration query int  false "Seconds that coupon is valid for"
// @Param discount query float64  false "Percentage discount"
// @Param target query string  false "Discounted item"
// @Success 200 {object} string
// @Failure 400 {object} httputil.HTTPError
// @Failure 404 {object} httputil.HTTPError
// @Failure 500 {object} httputil.HTTPError
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

func basket(c *gin.Context, id string, token string) {
	basket := LoadUser(id).Basket
	newBasket := recalculateBasket(basket)

	json.NewEncoder(c.Writer).Encode(newBasket)
}

func reset(c *gin.Context, id string, token string) {
	blankUser := User{Name: id, Basket: map[string]int{}, UsedCoupons: map[string]int{}, Orders: []map[string]float64{}}
	SaveUser(id, &blankUser)
	blankShop := Shop{Prices: map[string]float64{"apple": 1.0, "banana": 2.0, "pear": 3.0, "orange": 4.0, "fruit combo set": 18.0 * 0.7}, Coupons: map[string]Coupon{}}
	SaveStore(&blankShop)
}

func checkout(c *gin.Context, id string, token string) {
	coupon, _ := c.GetQuery("coupon")

	user := LoadUser(id)
	basket := recalculateBasket(user.Basket)
	order := calculateOrder(basket, coupon, user)

	json.NewEncoder(c.Writer).Encode(order)
}

// purchase godoc
// @Summary Complete checkout and purchase goods
// @Description Purchase items in basket
// @Tags purchase
// @Accept  json
// @Produce  json
// @Param coupon query string  false "Apply discont coupon"\
// @Success 200 {object} map[string]float64
// @Failure 400 {object} httputil.HTTPError
// @Failure 404 {object} httputil.HTTPError
// @Failure 500 {object} httputil.HTTPError

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
