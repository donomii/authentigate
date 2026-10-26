package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

func userFilename(id string) string {
	if id == "" || strings.ContainsAny(id, "/\\") {
		panic(fmt.Errorf("invalid user ID %q: expected a nonempty filename without path separators", id))
	}
	return fmt.Sprintf("userdata/%v.json", id)
}

func LoadUser(id string) *User {
	var out *User
	filename := userFilename(id)
	res, err := ioutil.ReadFile(filename)
	if os.IsNotExist(err) {
		t := User{Name: id, Basket: map[string]int{}, UsedCoupons: map[string]int{}, Orders: []map[string]float64{}}
		out = &t
	} else if err != nil {
		panic(fmt.Errorf("could not read user %q from %s: %w", id, filename, err))
	} else if err := json.Unmarshal(res, &out); err != nil {
		//panic(err)
		panic(fmt.Errorf("could not decode user %q from %s: %w", id, filename, err))
	} else if out == nil || out.Basket == nil || out.UsedCoupons == nil {
		panic(fmt.Errorf("invalid user %q in %s: expected basket and used coupon maps", id, filename))
	}
	return out
}

func SaveUser(id string, tasks *User) {
	payload, err := json.Marshal(tasks)
	if err != nil {
		panic(fmt.Errorf("could not encode user %q: %w", id, err))
	}
	writeJSON(userFilename(id), payload)
}

func LoadStore() *Shop {
	var out *Shop
	res, err := ioutil.ReadFile("shop.json")
	if os.IsNotExist(err) {
		t := Shop{Prices: map[string]float64{"apple": 1.0, "banana": 2.0, "pear": 3.0, "orange": 4.0, "fruit combo set": 18.0 * 0.7}, Coupons: map[string]Coupon{}}
		out = &t
	} else if err != nil {
		panic(fmt.Errorf("could not read shop.json: %w", err))
	} else if err := json.Unmarshal(res, &out); err != nil {
		//panic(err)
		panic(fmt.Errorf("could not decode shop.json: %w", err))
	} else if out == nil || out.Prices == nil || out.Coupons == nil {
		panic("invalid shop.json: expected price and coupon maps")
	}
	return out
}

func SaveStore(store *Shop) {
	payload, err := json.Marshal(store)
	if err != nil {
		panic(fmt.Errorf("could not encode shop.json: %w", err))
	}
	writeJSON("shop.json", payload)
}

func writeJSON(filename string, payload []byte) {
	file, err := os.CreateTemp(filepath.Dir(filename), ".authentigate-*.json")
	if err != nil {
		panic(fmt.Errorf("could not create temporary file for %s: %w", filename, err))
	}
	_, writeErr := file.Write(payload)
	closeErr := file.Close()
	if writeErr != nil {
		panic(fmt.Errorf("could not write %s: %w", filename, writeErr))
	} else if closeErr != nil {
		panic(fmt.Errorf("could not close temporary file for %s: %w", filename, closeErr))
	} else if err := os.Rename(file.Name(), filename); err != nil {
		panic(fmt.Errorf("could not replace %s with saved data: %w", filename, err))
	}
}
