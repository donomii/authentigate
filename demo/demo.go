package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/rpc"
)

type User struct {
	Name        string
	Basket      map[string]int
	UsedCoupons map[string]int
	Orders      []map[string]float64
}

func LoadJson(id string) *User {
	var out *User
	res, err := ioutil.ReadFile(fmt.Sprintf("userdata/%v.json", id))
	err = json.Unmarshal(res, &out)
	if err != nil {
		log.Println("Could not load user", err)
		//panic(err)
	}
	if out == nil {
		t := User{Name: id}
		out = &t
	}
	return out
}

func SaveJson(id string, tasks *User) {
	payload, err := json.Marshal(tasks)
	if err != nil {
		panic("Could not marshall quests")
	}
	ioutil.WriteFile(fmt.Sprintf("userdata/%v.json", id), payload, 0600)
}

type AddItemRequest struct {
	Id, Item string
	Amount   int
}

func (t *Shop) AddItem(args *AddItemRequest, reply *bool) error {
	user := LoadJson(args.Id)
	user.Basket[args.Item] = user.Basket[args.Item] + args.Amount
	SaveJson(args.Id, user)
	out := true
	reply = &out
	return nil
}

type BasketRequest struct {
	Id string
}

func (t *Shop) Basket(args *CheckoutRequest, reply *map[string]int) error {
	reply = &LoadJson(args.Id).Basket
	return nil
}

type CheckoutRequest struct {
	Id, Coupon string
}

func (t *Shop) Checkout(args *CheckoutRequest, reply *map[string]float64) error {
	price := map[string]float64{"apple": 1.0, "banana": 2.0, "pear": 3.0, "orange": 4.0, "fruit combo set": 18.0 * 0.7}
	user := LoadJson(args.Id)
	basket := user.Basket

	order := map[string]float64{}

	for item, count := range basket {
		order[item] = float64(count) * price[item]
	}
	if basket["apple"] > 6 {
		order["apple"] = 0.9 * float64(order["apple"])
	}
	if args.Coupon == "oranges" && user.UsedCoupons["oranges"] != 1 {
		order["orange"] = 0.7 * float64(order["orange"])
	}

	reply = &order

	return nil
}

func main() {
	shop := new(Shop)
	rpc.Register(shop)
	rpc.HandleHTTP()
	l, e := net.Listen("tcp", ":1234")
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}
