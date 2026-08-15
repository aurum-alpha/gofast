#!/usr/bin/env bash
# Canonical CI entrypoint: unit tests with coverage.
set -euo pipefail
cd "$(dirname "$0")/.."

go test ./... -cover
