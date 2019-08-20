#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
source ./lib.sh

if [[ ${CI:-} ]]; then
  apt-get update -qq
  apt-get install -qq python-pip > /dev/null
  # Need to add pip install directory to $PATH.
  export PATH="/home/circleci/.local/bin:$PATH"
  pip install -qqq autobahntestsuite
fi

# If you'd like to modify the args to go test, just run go test directly, this script is meant
# for running tests at the end to get coverage and test under the race detector.
go test -race -vet=off -coverprofile=ci/out/coverage.prof -coverpkg=./... ./...

if [[ ${CI:-} ]]; then
  bash <(curl -s https://codecov.io/bash) -f ci/out/coverage.prof
else
  go tool cover -html=ci/out/coverage.prof -o=ci/out/coverage.html
fi
