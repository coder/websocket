#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
cd "$(git rev-parse --show-toplevel)"

argv=(
  go run gotest.tools/gotestsum
  # https://circleci.com/docs/2.0/collect-test-data/
  "--junitfile=ci/out/websocket/testReport.xml"
  "--format=short-verbose"
  --
  -race
  "-vet=off"
  "-bench=."
)
# Interactive usage probably does not want to enable benchmarks, race detection
# turn off vet or use gotestsum by default.
if [[ $# -gt 0 ]]; then
  argv=(go test "$@")
fi

# We always want coverage.
argv+=(
  "-coverprofile=ci/out/coverage.prof"
  "-coverpkg=./..."
)

mkdir -p ci/out/websocket
"${argv[@]}"

# Removes coverage of generated files.
grep -v _string.go < ci/out/coverage.prof > ci/out/coverage2.prof
mv ci/out/coverage2.prof ci/out/coverage.prof

go tool cover -html=ci/out/coverage.prof -o=ci/out/coverage.html
if [[ ${CI:-} ]]; then
  bash <(curl -s https://codecov.io/bash) -f ci/out/coverage.prof
fi
