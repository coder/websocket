#!/usr/bin/env bash

source ci/lib.sh || exit 1

(
	shopt -s globstar nullglob dotglob
	shellcheck ./**/*.sh
)

go vet -composites=false ./...
go run golang.org/x/lint/golint -set_exit_status ./...
