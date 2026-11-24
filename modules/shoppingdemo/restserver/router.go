package main

import "github.com/gin-gonic/gin"

func configureRoutes(router *gin.Engine, prefix string) {
	router.GET(prefix+"basket", makeAuthed(basket))
	router.GET(prefix+"checkout", makeAuthed(checkout))
	router.POST(prefix+"purchase", makeAuthed(purchase))
	router.POST(prefix+"addItem", makeAuthed(addItem))
	router.POST(prefix+"createCoupon", makeAuthed(createCoupon))
	router.GET(prefix+"listCoupon", makeAuthed(listCoupons))
	router.GET(prefix+"listCoupons", makeAuthed(listCoupons))
	router.POST(prefix+"deleteCoupon", makeAuthed(deleteCoupon))
	router.POST(prefix+"reset", makeAuthed(reset))
	router.GET(prefix+"listItems", makeAuthed(listItems))
}
