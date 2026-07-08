#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
go mod tidy
go build -buildmode=c-shared -ldflags "-s -w" -o hi-on-json.so .
echo "Built: $(pwd)/hi-on-json.so"
