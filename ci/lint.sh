#!/usr/bin/env bash
set -euo pipefail

main() {
  cd "$(dirname "$0")/.."

  go vet ./...
  GOOS=js GOARCH=wasm go vet ./...

  golint -set_exit_status ./...
  GOOS=js GOARCH=wasm golint -set_exit_status ./...

  shellcheck --exclude=SC2046 $(git ls-files "*.sh")
}

main "$@"
