#!/usr/bin/env bash
set -euo pipefail

main() {
  cd "$(dirname "$0")/.."

  go test -timeout=30m -covermode=atomic -coverprofile=ci/out/coverage.prof -coverpkg=./... "$@" ./...
  sed -i '/stringer\.go/d' ci/out/coverage.prof
  sed -i '/github.com\/fortytw2\/websocket\/internal\/test/d' ci/out/coverage.prof
  sed -i '/examples/d' ci/out/coverage.prof

  # Last line is the total coverage.
  go tool cover -func ci/out/coverage.prof | tail -n1

  go tool cover -html=ci/out/coverage.prof -o=ci/out/coverage.html
}

main "$@"
