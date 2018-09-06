#!/usr/bin/env bash

source .github/lib.sh

if [[ $(gofmt -l -s .) != "" ]]; then
	echo "files are not formatted correctly"
	echo "please run:"
	echo "gofmt -w -s ."
	exit 1
fi

out=$(go run golang.org/x/tools/cmd/goimports -l -local=nhooyr.io/ws .)
if [[ $out != "" ]]; then
	echo "imports are not formatted correctly"
	echo "please run:"
	echo "goimports -w -local=nhooyr.io/ws ."
	exit 1
fi

out=$(go run mvdan.cc/sh/cmd/shfmt -l -s -sr .)
if [[ $out != "" ]]; then
	echo "shell scripts are not formatted correctly"
	echo "please run:"
	echo "shfmt -w -s -sr ."
	exit 1
fi
