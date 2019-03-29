#!/usr/bin/env bash

source .github/lib.sh || exit 1

COVERAGE_PROFILE=$(mktemp)
go test -race -v "-coverprofile=${COVERAGE_PROFILE}" -vet=off ./...
go tool cover "-func=${COVERAGE_PROFILE}"

if [[ $CI ]]; then
	bash <(curl -s https://codecov.io/bash) -f "$COVERAGE_PROFILE"
else
	go tool cover "-html=${COVERAGE_PROFILE}" -o=coverage.html

	set +x
	echo
	echo "please open coverage.html to see detailed test coverage stats"
fi
