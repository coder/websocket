#!/usr/bin/env bash

source ci/lib.sh || exit 1

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
	go run mvdan.cc/sh/cmd/shfmt -w -s -sr .
}

gen
fmt

if [[ $CI && $(unstaged_files) != "" ]]; then
	set +x
	echo
	echo "files either need generation or are formatted incorrectly"
	echo "please run:"
	echo "./ci/run.sh"
	echo
	git status
	exit 1
fi
