#!/bin/bash

echo "Building with vendored dependencies..."

# Vendor dependencies first
go mod vendor

# Build static binary
CGO_ENABLED=0 \
GOOS=linux \
GOARCH=amd64 \
go build \
    -mod=vendor \
    -a \
    -ldflags="-s -w -extldflags '-static'" \
    -trimpath \
    -installsuffix cgo \
    -o dlog

# Clean up
rm -rf vendor/

echo "Built: $(du -h dlog | cut -f1)"
ldd dlog 2>&1 || file dlog