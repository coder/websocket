#!/usr/bin/env bash
set -euo pipefail

main() {
  cd "$(dirname "$0")/.."

  ./ci/fmt.sh
  ./ci/lint.sh
  ./ci/test.sh "$@"
}

main "$@"
