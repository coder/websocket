#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
cd "$(git rev-parse --show-toplevel)"

GOOS=js GOARCH=wasm go vet ./...

go install golang.org/x/lint/golint
GOOS=js GOARCH=wasm golint -set_exit_status ./...

wsjstestOut="$(mktemp -d)/stdout"
mkfifo "$wsjstestOut"
go install ./internal/wsjstest
timeout 30s wsjstest > "$wsjstestOut" &
wsjstestPID=$!

WS_ECHO_SERVER_URL="$(timeout 10s head -n 1 "$wsjstestOut")" || true
if [[ -z $WS_ECHO_SERVER_URL ]]; then
  echo "./internal/wsjstest failed to start in 10s"
  exit 1
fi

go install github.com/agnivade/wasmbrowsertest
GOOS=js GOARCH=wasm go test -exec=wasmbrowsertest ./... -args "$WS_ECHO_SERVER_URL"

if ! wait "$wsjstestPID"; then
  echo "wsjstest exited unsuccessfully"
  echo "output:"
  cat "$wsjstestOut"
  exit 1
fi
