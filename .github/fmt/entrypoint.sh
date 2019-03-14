#!/usr/bin/env bash

set -euxo pipefail

export GO111MODULE=on
export GOFLAGS=-mod=readonly

if [[ $(gofmt -l -s .) != "" ]]; then
	echo "files are not formatted correctly"
	echo "please run:"
	echo "gofmt -s ."
fi

go get -u golang.org/x/tools/cmd/goimports

if [[ $(goimports -l -local=nhooyr.io/ws .) != "" ]]; then
	echo "imports are not formatted correctly"
	echo "please run:"
	echo "goimports -local=nhooyr.io/ws ."
fi
