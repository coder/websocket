#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
source ./lib.sh

# shellcheck disable=SC2046
shellcheck -e SC1091 -x $(git ls-files "*.sh")
go vet ./...
go run golang.org/x/lint/golint -set_exit_status ./...
