#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
source ./lib.sh

unstaged_files() {
  git ls-files --other --modified --exclude-standard
}

gen() {
  # Unfortunately, this is the only way to ensure go.mod and go.sum are correct.
  # See https://github.com/golang/go/issues/27005
  go list ./... > /dev/null
  go mod tidy

  go generate ./...
}

fmt() {
  gofmt -w -s .
  go run go.coder.com/go-tools/cmd/goimports -w "-local=$(go list -m)" .
  go run mvdan.cc/sh/cmd/shfmt -i 2 -w -s -sr .
}

gen
fmt

if [[ $CI && $(unstaged_files) != "" ]]; then
  echo
  echo "Files either need generation or are formatted incorrectly."
  echo "Please run:"
  echo "./ci/fmt.sh"
  echo
  git status
  exit 1
fi
