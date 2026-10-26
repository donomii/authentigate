#!/bin/sh

set -eu
cd "$(dirname "$0")"
go test -race ./...
go vet ./...
for module in vhoster modules/cameraview modules/shoppingdemo/restserver modules/shoppingdemo/frontend; do
    (cd "$module" && go test -race ./... && go vet ./...)
done
