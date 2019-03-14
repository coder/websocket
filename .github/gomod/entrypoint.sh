#!/usr/bin/env bash

set -euxo pipefail

export GO111MODULE=on
export GOFLAGS=-mod=readonly

function help() {
	echo
	echo "you may need to update go.mod/go.sum via:"
	echo "go list all > /dev/null"
	echo "go mod tidy"
	exit 1
}

go list -mod=readonly all > /dev/null || help
go mod tidy || help

# Until https://github.com/golang/go/issues/27005 the previous command can actually modify go.sum so we need to ensure its not changed.
if [[ $(git diff --name-only) != "" ]]; then
	git diff
	help
fi
