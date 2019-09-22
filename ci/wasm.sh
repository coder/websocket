#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
cd "$(git rev-parse --show-toplevel)"

GOOS=js GOARCH=wasm go vet ./...

go install golang.org/x/lint/golint
GOOS=js GOARCH=wasm golint -set_exit_status ./...

wsEchoOut="$(mktemp -d)/stdout"
mkfifo "$wsEchoOut"
go install ./internal/wsecho/cmd/wsecho
wsecho > "$wsEchoOut" &

WS_ECHO_SERVER_URL="$(timeout 10s head -n 1 "$wsEchoOut")" || true
if [[ -z $WS_ECHO_SERVER_URL ]]; then
  echo "./internal/wsecho/cmd/wsecho failed to start in 10s"
  exit 1
fi

go install github.com/agnivade/wasmbrowsertest
GOOS=js GOARCH=wasm go test -exec=wasmbrowsertest ./... -args "$WS_ECHO_SERVER_URL"

kill %1
wait -n || true
