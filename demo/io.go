package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
)

func LoadUser(id string) *User {
	var out *User
	res, err := ioutil.ReadFile(fmt.Sprintf("userdata/%v.json", id))
	err = json.Unmarshal(res, &out)
	if err != nil {
		log.Println("Could not load user", err)
		//panic(err)
	}
	if out == nil {
		t := User{Name: id, Basket: map[string]int{}, UsedCoupons: map[string]int{}, Orders: []map[string]float64{}}
		out = &t
	}
	return out
}

func SaveUser(id string, tasks *User) {
	payload, err := json.Marshal(tasks)
	if err != nil {
		panic("Could not marshall user")
	}
	ioutil.WriteFile(fmt.Sprintf("userdata/%v.json", id), payload, 0600)
}

func LoadStore() *Shop {
	var out *Shop
	res, err := ioutil.ReadFile(fmt.Sprintf("shop.json"))
	err = json.Unmarshal(res, &out)
	if err != nil {
		log.Println("Could not load shop", err)
		//panic(err)
	}
	if out == nil {
		t := Shop{Prices: map[string]float64{"apple": 1.0, "banana": 2.0, "pear": 3.0, "orange": 4.0, "fruit combo set": 18.0 * 0.7}, Coupons: map[string]Coupon{}}
		out = &t
	}
	return out
}

func SaveStore(store *Shop) {
	payload, err := json.Marshal(store)
	if err != nil {
		panic("Could not marshall shop")
	}
	ioutil.WriteFile(fmt.Sprintf("shop.json"), payload, 0600)
}
