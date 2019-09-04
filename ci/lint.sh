#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
cd "$(git rev-parse --show-toplevel)"

# shellcheck disable=SC2046
shellcheck -x $(git ls-files "*.sh")
go vet ./...
go run golang.org/x/lint/golint -set_exit_status ./...
