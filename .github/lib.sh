#!/usr/bin/env bash

set -euxo pipefail || exit 1

export GO111MODULE=on
export PAGER=cat

# shellcheck disable=SC2034
# CI is used by the scripts that source this file.
export CI=${GITHUB_ACTION-}

if [[ $CI ]]; then
	export GOFLAGS=-mod=readonly
fi

unstaged_files() {
	git ls-files --other --modified --exclude-standard
}
