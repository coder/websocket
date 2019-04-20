#!/usr/bin/env bash

# This script is for local testing. See .github for CI.

cd "$(dirname "${0}")/.." || exit 1
source ci/lib.sh || exit 1

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

function help() {
	set +x
	echo
	echo "$0 [-h] <step>"
	cat << EOF

If you do not pass in an explicit step, all steps will be ran in order.
Pass "analyze" as the step to be put into an interactive container to analyze
profiles.
EOF
	exit 1
}

# Use this to analyze benchmark profiles.
if [[ ${1-} == "analyze" ]]; then
	docker run \
		-it \
		-v "${PWD}:/repo" \
		-v "$(go env GOPATH):/go" \
		-v "$(go env GOCACHE):/root/.cache/go-build" \
		-w /repo \
		golang:1.12
fi

if [[ ${1-} == "-h" || ${1-} == "--help" || ${1-} == "help" ]]; then
	help
fi

if [[ $# -gt 0 ]]; then
	if [[ ! -d "ci/$*" ]]; then
		help
	fi

	docker_run "ci/$*"
	exit 0
fi

docker_run ci/fmt
docker_run ci/lint
docker_run ci/test
docker_run ci/bench
