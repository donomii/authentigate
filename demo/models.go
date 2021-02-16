package main

import "time"

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
