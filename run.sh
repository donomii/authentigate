#!/bin/sh

set -eu
cd "$(dirname "$0")"

echo building authentigate
go build .
echo starting authentigate
./authentigate -develop &

cd modules
echo starting modules
./run.sh &

wait
