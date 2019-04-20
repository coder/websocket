#!/usr/bin/env bash

source ci/lib.sh || exit 1

mkdir -p profs

set +x
echo "this step includes benchmarks for race detection and coverage purposes
but the numbers will be misleading. please see the bench step for more
accurate numbers"
set -x

go test -race -coverprofile=profs/coverage --vet=off -bench=. ./...
go tool cover -func=profs/coverage

if [[ $CI ]]; then
	bash <(curl -s https://codecov.io/bash) -f profs/coverage
else
	go tool cover -html=profs/coverage -o=coverage.html

	set +x
	echo
	echo "please open coverage.html to see detailed test coverage stats"
	echo "profiles are in ./prof/"
fi
