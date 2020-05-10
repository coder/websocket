#!/usr/bin/env bash
set -euo pipefail

main() {
  cd "$(dirname "$0")/.."

  go test -timeout=30m -covermode=atomic -coverprofile=ci/out/coverage.prof -coverpkg=./... "$@" ./...
  sed -i '/stringer\.go/d' ci/out/coverage.prof
  sed -i '/nhooyr.io\/websocket\/internal\/test/d' ci/out/coverage.prof
  sed -i '/examples/d' ci/out/coverage.prof

  # Last line is the total coverage.
  go tool cover -func ci/out/coverage.prof | tail -n1

  go tool cover -html=ci/out/coverage.prof -o=ci/out/coverage.html

  if [[ ${CI-} && ${GITHUB_REF-} == *master ]]; then
    local deployDir
    deployDir="$(mktemp -d)"
    cp ci/out/coverage.html "$deployDir/index.html"
    netlify deploy --prod "--dir=$deployDir"
  fi
}

main "$@"
