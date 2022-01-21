#!/bin/sh

go get github.com/zsais/go-gin-prometheus

cd shoppingdemo
./run.sh &
cd ..

cd menu
go build .
./menu &
cd ..

cd presence
go build .
./presence
