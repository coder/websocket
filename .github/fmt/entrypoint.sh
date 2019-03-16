#!/usr/bin/env bash

source .github/lib.sh

gofmt -w -s .
go run go.coder.com/go-tools/cmd/goimports -w "-local=$(go list -m)" .
go run mvdan.cc/sh/cmd/shfmt -w -s -sr .

if [[ $CI ]] && unstaged_files; then
	set +x
	echo
	echo "files are not formatted correctly"
	echo "please run:"
	echo "./test.sh"
	echo
	git status
	exit 1
fi
