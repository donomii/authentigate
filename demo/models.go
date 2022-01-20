package main

import "time"

//Stores all user related data
// swagger:model
type User struct {
	Name        string `json:"name"`
	Basket      map[string]int
	UsedCoupons map[string]int
	Orders      []map[string]float64
}

//A discount coupon
// swagger:model
type Coupon struct {
	Name     string
	Expiry   time.Time
	Target   string
	Discount float64
}

//Holds shop data, including coupons and prices
// swagger:model
type Shop struct {
	Prices  map[string]float64
	Coupons map[string]Coupon
}
