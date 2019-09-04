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
  "-vet=off"
)
# Interactive usage does not want to turn off vet or use gotestsum.
if [[ $# -gt 0 ]]; then
  argv=(go test "$@")
fi

# We always want coverage and race detection.
argv+=(
  "-parallel=512"
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
  bash <(curl -s https://codecov.io/bash) -R . -f ci/out/coverage.prof
fi
