#!/bin/sh

set -eu
cd "$(dirname "$0")"
go build ./...
for module in vhoster modules/shoppingdemo/restserver modules/shoppingdemo/frontend; do
    (cd "$module" && go build ./...)
done
