#!/bin/bash

echo "Building with vendored dependencies..."

# Vendor dependencies first
go mod vendor

PLATFORM="$1"
PROCESSOR="$2"
ARCHITECT="$3"

if [ -z "$PLATFORM" ]; then
    echo "please define platform (linux/darwin/windows)"
    exit 1
fi
if [ -z "$PROCESSOR" ]; then
    echo "please define processor (amd/arm)"
    exit 1
fi
if [ -z "$ARCHITECT" ]; then
    echo "please define architect (32/64)"
    exit 1
fi

# Compose GOARCH properly
GOARCH=""
if [ "$PROCESSOR" = "amd" ] && [ "$ARCHITECT" = "64" ]; then
    GOARCH="amd64"
elif [ "$PROCESSOR" = "amd" ] && [ "$ARCHITECT" = "32" ]; then
    GOARCH="386"
elif [ "$PROCESSOR" = "arm" ] && [ "$ARCHITECT" = "64" ]; then
    GOARCH="arm64"
elif [ "$PROCESSOR" = "arm" ] && [ "$ARCHITECT" = "32" ]; then
    GOARCH="arm"
else
    echo "Unsupported processor/architecture combination: $PROCESSOR/$ARCHITECT"
    exit 1
fi

# Build static binary
CGO_ENABLED=0 \
GOOS="$PLATFORM" \
GOARCH="$GOARCH" \
go build -mod=vendor \
    -a \
    -ldflags="-s -w -extldflags '-static'" \
    -trimpath \
    -installsuffix cgo \
    -o dlog \
    ./cmd
# Clean up
rm -rf vendor/

echo "Built: $(du -h dlog | cut -f1)"
ldd dlog 2>&1 || file dlog
