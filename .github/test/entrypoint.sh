#!/usr/bin/env bash

source .github/lib.sh

function gomod_help() {
	echo
	echo "you may need to update go.mod/go.sum via:"
	echo "go list all > /dev/null"
	echo "go mod tidy"
	echo
	echo "or git add files to staging"
	exit 1
}

go list ./... > /dev/null || gomod_help
go mod tidy

# Until https://github.com/golang/go/issues/27005 the previous command can actually modify go.sum so we need to ensure its not changed.
if [[ $(git diff --name-only) != "" ]]; then
	git diff
	gomod_help
fi

mapfile -t scripts <<< "$(find . -type f -name "*.sh")"
shellcheck "${scripts[@]}"

go vet -composites=false ./...

go test -race -v -coverprofile=coverage.out -vet=off ./...

if [[ -z ${GITHUB_ACTION-} ]]; then
	go tool cover -html=coverage.out
else
	bash <(curl -s https://codecov.io/bash)
fi

rm coverage.out
