#!/bin/sh

echo building authentigate
go build .
echo starting authentigate
./authentigate -develop &

cd modules/menu
echo building menu
go build .
echo starting menu
./menu &

cd ..
cd shoppingdemo
echo starting shoppingmenu
/bin/sh run.sh &

wait
