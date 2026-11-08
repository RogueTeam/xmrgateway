#!/bin/sh

mkdir -p build

CGO_ENABLED=0 GOOS=openbsd GOARCH=amd64 go build \
    -trimpath \
    -ldflags='-s -w' \
    -o build/xmrgateway-amd64-openbsd \
    ./cmd/xmrgateway

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags='-s -w' \
    -o build/xmrgateway-amd64-linux    \
    ./cmd/xmrgateway
