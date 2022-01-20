package main

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

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
