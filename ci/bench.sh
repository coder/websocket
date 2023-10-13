#!/bin/sh
set -eu
cd -- "$(dirname "$0")/.."

go test --run=^$ --bench=. "$@" ./...
(
  cd ./internal/thirdparty
  go test --run=^$ --bench=. "$@" ./...
)
