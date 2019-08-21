#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
cd "$(git rev-parse --show-toplevel)"

mkdir -p ci/out/gotest
testFlags=(-race "-vet=off" "-coverprofile=ci/out/coverage.prof" "-coverpkg=./...")
if [[ ${CI-} ]]; then
  # https://circleci.com/docs/2.0/collect-test-data/
  go test "${testFlags[@]}" -v ./... 2>&1 | tee /dev/stderr |
    go run github.com/jstemmer/go-junit-report > ci/out/gotest/report.xml
else
  go test "${testFlags[@]}" ./...
fi

go tool cover -html=ci/out/coverage.prof -o=ci/out/coverage.html
if [[ ${CI:-} ]]; then
  bash <(curl -s https://codecov.io/bash) -f ci/out/coverage.prof
fi
