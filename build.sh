#!/bin/bash

echo "Building with vendored dependencies..."

# Vendor dependencies first
go mod vendor
PLATFORM=$1
PROCESSOR=$2
ARCHITECT=$3
if [ -z "$PLATFORM"]; then
    echo "please define platform linux/cross"
    exit 1
fi
if [ -z "$PROCESSOR"]; then
    echo "please define processor amd/arm"
    exit 1
fi
if [ -z "$ARCHITECT"]; then
    echo "please define architect 32/64"
    exit 1
fi
# Build static binary
CGO_ENABLED=0 \
GOOS="$PLATFORM" \
GOARCH="$PROCESSOR""$ARCHITECT" \
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