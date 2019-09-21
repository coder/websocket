#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
cd "$(git rev-parse --show-toplevel)"

stdout="$(mktemp -d)/stdout"
mkfifo "$stdout"
go run ./internal/wsecho/cmd > "$stdout" &

WS_ECHO_SERVER_URL="$(head -n 1 "$stdout")"

GOOS=js GOARCH=wasm go vet ./...
go install golang.org/x/lint/golint
GOOS=js GOARCH=wasm golint -set_exit_status ./...
GOOS=js GOARCH=wasm go test ./... -args "$WS_ECHO_SERVER_URL"
