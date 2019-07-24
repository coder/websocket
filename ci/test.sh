#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
source ./lib.sh

echo "This step includes benchmarks for race detection and coverage purposes
but the numbers will be misleading. please see the bench step or ./bench.sh for
more accurate numbers."
echo

if [[ $CI ]]; then
  apt-get update -qq
  apt-get install -qq python-pip > /dev/null
  # Need to add pip install directory to $PATH.
  export PATH="/home/circleci/.local/bin:$PATH"
  pip install -qqq autobahntestsuite
fi

go test -race -coverprofile=ci/out/coverage.prof --vet=off -bench=. -coverpkg=./... "$@" ./...
go tool cover -func=ci/out/coverage.prof

if [[ $CI ]]; then
  bash <(curl -s https://codecov.io/bash) -f ci/out/coverage.prof
else
  go tool cover -html=ci/out/coverage.prof -o=ci/out/coverage.html

  echo
  echo "Please open ci/out/coverage.html to see detailed test coverage stats."
fi
