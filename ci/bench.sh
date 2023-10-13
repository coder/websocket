#!/bin/sh
set -eu
cd -- "$(dirname "$0")/.."

go test --bench=. "$@" ./...
(
  cd ./internal/thirdparty
  go test --bench=. "$@" ./...
)
