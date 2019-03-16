#!/usr/bin/env bash

# This is for local testing. See .github for CI.

source ./.github/lib.sh

function docker_run() {
	local dir=$1
	docker run \
		-v "${PWD}:/repo" \
		-v "$(go env GOPATH):/go" \
		-v "$(go env GOCACHE):/root/.cache/go-build" \
		-w /repo \
		"$(docker build -q "$dir")"
}
docker_run .github/fmt
docker_run .github/test

set +x
echo "please open coverage.html to see detailed test coverage stats"
