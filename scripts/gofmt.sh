#!/usr/bin/env bash
# Canonical CI entrypoint: fail if any file is not gofmt-clean.
set -euo pipefail
cd "$(dirname "$0")/.."

unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
  echo "The following files are not gofmt-clean:"
  echo "$unformatted"
  exit 1
fi
