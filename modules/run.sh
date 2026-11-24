#!/bin/sh
set -eu

script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
cd "$script_dir"

go build -mod=readonly -o menu/menu ./menu
go build -mod=readonly -o presence/presence ./presence

cd shoppingdemo
./run.sh &
cd ..

cd menu
./menu &
cd ..

cd presence
./presence
