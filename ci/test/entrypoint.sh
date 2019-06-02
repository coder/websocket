#!/usr/bin/env bash

source ci/lib.sh || exit 1

set +x
echo
echo "this step includes benchmarks for race detection and coverage purposes
but the numbers will be misleading. please see the bench step for more
accurate numbers"
echo
set -x

go test -race -coverprofile=ci/out/coverage.prof --vet=off -bench=. -coverpkg=./... ./...
go tool cover -func=ci/out/coverage.prof

if [[ $CI ]]; then
	bash <(curl -s https://codecov.io/bash) -f ci/out/coverage.prof
else
	go tool cover -html=ci/out/coverage.prof -o=ci/out/coverage.html

	set +x
	echo
	echo "please open ci/out/coverage.html to see detailed test coverage stats"
fi
