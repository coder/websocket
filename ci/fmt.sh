#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
cd "$(git rev-parse --show-toplevel)"

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
  # shellcheck disable=SC2046
  npx prettier \
    --write \
    --print-width 120 \
    --no-semi \
    --trailing-comma all \
    --loglevel silent \
    $(git ls-files "*.yaml" "*.yml" "*.md")
}

unstaged_files() {
  git ls-files --other --modified --exclude-standard
}

check() {
  if [[ ${CI:-} && $(unstaged_files) != "" ]]; then
    echo
    echo "Files need generation or are formatted incorrectly."
    echo "Run:"
    echo "./ci/fmt.sh"
    echo
    git status
    git diff
    exit 1
  fi
}

gen
fmt
check
