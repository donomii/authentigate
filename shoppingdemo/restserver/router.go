package main

import "github.com/gin-gonic/gin"

func configureRoutes(router *gin.Engine, prefix string) {
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
