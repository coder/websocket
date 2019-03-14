#!/usr/bin/env bash

source .github/lib.sh

if [[ $(gofmt -l -s .) != "" ]]; then
	echo "files are not formatted correctly"
	echo "please run:"
	echo "gofmt -w -s ."
	exit 1
fi

go install golang.org/x/tools/cmd/goimports

if [[ $(goimports -l -local=nhooyr.io/ws .) != "" ]]; then
	echo "imports are not formatted correctly"
	echo "please run:"
	echo "goimports -w -local=nhooyr.io/ws ."
	exit 1
fi

go install mvdan.cc/sh/cmd/shfmt

if [[ $(shfmt -l -s .) != "" ]]; then
	echo "shell scripts are not formatted correctly"
	echo "please run:"
	echo "shfmt -w -s ."
	exit 1
fi
