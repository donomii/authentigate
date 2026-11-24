#!/bin/sh
set -eu

script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
cd "$script_dir"
mkdir -p -m 700 restserver/userdata

cd restserver
go build -mod=readonly -o restserver .
cd ..
cd frontend
go build -mod=readonly -o frontend .
cd ../restserver
./restserver &
cd ../frontend
./frontend
