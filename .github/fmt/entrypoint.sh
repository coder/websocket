#!/usr/bin/env bash

source .github/lib.sh || exit 1

gen() {
	# Unfortunately, this is the only way to ensure go.mod and go.sum are correct.
	# See https://github.com/golang/go/issues/27005
	go list ./... > /dev/null
	go mod tidy

	go install github.com/golang/protobuf/protoc-gen-go
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
	echo "./test.sh"
	echo
	git status
	exit 1
fi
