package main

//swagger:parameters createCoupon updateCoupon
type createCouponRequest struct {
	Duration int
	Discount float64
	Target   string
}

//swagger:parameters addItem
type addItemRequest struct {
	Item   string
	Amount int
}

//swagger:parameters purchase
type purchaseRequest struct {
	Coupon string
}

//swagger:parameters deleteCoupon
type deleteCouponRequest struct {
	Coupon string
}
