#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
cd "$(git rev-parse --show-toplevel)"

GOOS=js GOARCH=wasm go vet ./...

go install golang.org/x/lint/golint
GOOS=js GOARCH=wasm golint -set_exit_status ./...

wsjstestOut="$(mktemp)"
go install ./internal/wsjstest
timeout 30s wsjstest >> "$wsjstestOut" 2>&1 &
wsjstestPID=$!

# See https://superuser.com/a/900134
WS_ECHO_SERVER_URL="$( (tail -f -n0 "$wsjstestOut" &) | timeout 10s head -n 1)"
if [[ -z $WS_ECHO_SERVER_URL ]]; then
  echo "./internal/wsjstest failed to start in 10s"
  exit 1
fi

go install github.com/agnivade/wasmbrowsertest
export WS_ECHO_SERVER_URL
GOOS=js GOARCH=wasm go test -exec=wasmbrowsertest ./...

kill "$wsjstestPID"
if ! wait "$wsjstestPID"; then
  echo "--- wsjstest exited unsuccessfully"
  echo "output:"
  cat "$wsjstestOut"
  exit 1
fi
