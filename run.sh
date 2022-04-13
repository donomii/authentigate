#!/bin/sh

echo building authentigate
go build .
echo starting authentigate
./authentigate -develop &

cd modules
echo starting modules
./run.sh &

wait
