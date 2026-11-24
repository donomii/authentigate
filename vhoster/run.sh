#!/bin/sh

set -eu
cd "$(dirname "$0")"
go build -o vhoster .
exec ./vhoster -develop
