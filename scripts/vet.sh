#!/usr/bin/env bash
# Canonical CI entrypoint: go vet.
set -euo pipefail
cd "$(dirname "$0")/.."

go vet ./...
