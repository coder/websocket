#!/usr/bin/env bash

set -euo pipefail

WS_ECHO_SERVER_URL="$(wsjstest)"
trap 'pkill -KILL wsjstest' EXIT INT
export WS_ECHO_SERVER_URL

GOOS=js GOARCH=wasm go test -exec=wasmbrowsertest ./...
