#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
cd "$(git rev-parse --show-toplevel)"

argv+=(go test
  "-parallel=1024"
  "-coverprofile=ci/out/coverage.prof"
  "-coverpkg=./..."
)

if [[ ${CI-} ]]; then
  argv+=(
    "-race"
  )
fi

if [[ $# -gt 0 ]]; then
  argv+=(
    "$@"
  )
else
  argv+=(./...)
fi

mkdir -p ci/out/websocket
"${argv[@]}"

# Removes coverage of generated files.
grep -v _string.go < ci/out/coverage.prof > ci/out/coverage2.prof
mv ci/out/coverage2.prof ci/out/coverage.prof

go tool cover -html=ci/out/coverage.prof -o=ci/out/coverage.html
if [[ ${CI-} ]]; then
  bash <(curl -s https://codecov.io/bash) -Z -R . -f ci/out/coverage.prof
fi
