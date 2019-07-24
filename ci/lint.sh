#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
source ./lib.sh

if [[ $CI ]]; then
  apt-get update -qq
  apt-get install -qq shellcheck > /dev/null
fi

# shellcheck disable=SC2046
shellcheck -e SC1091 -x $(git ls-files "*.sh")
go vet ./...
go run golang.org/x/lint/golint -set_exit_status ./...
