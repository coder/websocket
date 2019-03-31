#!/usr/bin/env bash

# This script is for local testing. See .github for CI.

source .github/lib.sh || exit 1
cd "$(dirname "${0}")"

function docker_run() {
	local DIR="$1"
	local IMAGE
	IMAGE="$(docker build -q "$DIR")"
	docker run \
		-it \
		-v "${PWD}:/repo" \
		-v "$(go env GOPATH):/go" \
		-v "$(go env GOCACHE):/root/.cache/go-build" \
		-w /repo \
		"${IMAGE}"
}

if [[ $# -gt 0 ]]; then
	docker_run ".github/$*"
	exit 0
fi

docker_run .github/fmt
docker_run .github/lint
docker_run .github/test
