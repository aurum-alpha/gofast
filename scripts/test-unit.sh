#!/usr/bin/env bash
# Canonical CI entrypoint: unit tests with coverage.
set -euo pipefail
cd "$(dirname "$0")/.."

mkdir -p coverage
go test ./... -coverprofile=coverage/go-unit.out -covermode=atomic
