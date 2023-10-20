#!/bin/sh
set -eu
cd -- "$(dirname "$0")/.."

(
  cd ./internal/examples
  go test "$@" ./...
)
(
  cd ./internal/thirdparty
  go test "$@" ./...
)

go install github.com/agnivade/wasmbrowsertest@latest
go test --race --bench=. --timeout=1h --covermode=atomic --coverprofile=ci/out/coverage.prof --coverpkg=./... "$@" ./...
sed -i.bak '/stringer\.go/d' ci/out/coverage.prof
sed -i.bak '/nhooyr.io\/websocket\/internal\/test/d' ci/out/coverage.prof
sed -i.bak '/examples/d' ci/out/coverage.prof

# Last line is the total coverage.
go tool cover -func ci/out/coverage.prof | tail -n1

go tool cover -html=ci/out/coverage.prof -o=ci/out/coverage.html
