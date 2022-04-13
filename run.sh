#!/bin/sh

echo building authentigate
rm authentigate
go build .
echo starting authentigate
./authentigate -develop &

cd modules
echo starting modules
./run.sh &

wait
