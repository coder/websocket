#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
cd "$(git rev-parse --show-toplevel)"

GOOS=js GOARCH=wasm go vet ./...
go install golang.org/x/lint/golint
# Get passing later.
#GOOS=js GOARCH=wasm golint -set_exit_status ./...
GOOS=js GOARCH=wasm go test ./internal/wsjs
