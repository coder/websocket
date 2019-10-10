#!/usr/bin/env bash

set -euo pipefail

wsjstestOut="$(mktemp -d)/wsjstestOut"
mkfifo "$wsjstestOut"
timeout 45s wsjstest > "$wsjstestOut" &
wsjstestPID="$!"

WS_ECHO_SERVER_URL="$(head -n 1 "$wsjstestOut")"
export WS_ECHO_SERVER_URL

GOOS=js GOARCH=wasm go test -exec=wasmbrowsertest ./...

if ! wait "$wsjstestPID" ; then
  echo "wsjstest exited unsuccessfully"
  exit 1
fi
