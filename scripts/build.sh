#!/usr/bin/env bash
# Compile all packages and the release binaries, stamping internal/version.
# The React app is NOT needed here: it ships as files in the container image,
# not compiled into the binary, so this builds standalone.
set -euo pipefail
cd "$(dirname "$0")/.."

BUILD_NUMBER="${BUILD_NUMBER:-local}"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse --short=7 HEAD 2>/dev/null || echo unknown)}"
BUILT_AT="${BUILT_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

LDFLAGS="-s -w \
  -X github.com/j27-aurum/gofast/internal/version.Build=${BUILD_NUMBER} \
  -X github.com/j27-aurum/gofast/internal/version.Commit=${GIT_COMMIT} \
  -X github.com/j27-aurum/gofast/internal/version.BuiltAt=${BUILT_AT}"

go build ./...
mkdir -p bin
CGO_ENABLED=0 go build -trimpath -ldflags="$LDFLAGS" -o bin/fastgen ./cmd/fastgen
CGO_ENABLED=0 go build -trimpath -ldflags="$LDFLAGS" -o bin/fastproxy ./cmd/fastproxy
chmod 755 bin/fastgen bin/fastproxy
