#!/bin/sh

go get github.com/zsais/go-gin-prometheus

cd shoppingdemo
./run.sh &
cd ..

cd menu
rm menu
go build .
./menu &
cd ..

cd presence
rm presence
go build .
./presence
