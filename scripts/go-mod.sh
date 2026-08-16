#!/usr/bin/env bash
# Canonical CI entrypoint: download and verify Go modules.
set -euo pipefail
cd "$(dirname "$0")/.."

go mod download
go mod verify
