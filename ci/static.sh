#!/usr/bin/env bash
set -eu

cd -- "$(dirname "$0")/.."

if [[ ! -f ./ci/out/profile.txt ]]; then
	echo "No coverage profile found, run 'make test' first"
	exit 1
fi

if [[ ! -f ./ci/out/coverage.html ]]; then
	echo "No coverage report found, run 'make test' first"
	exit 1
fi

rm -rf ./ci/out/static
mkdir -p ./ci/out/static
cp ./ci/out/coverage.html ./ci/out/static/coverage.html
percent=$(go tool cover -func ./ci/out/profile.txt | tail -n1 | awk '{print $3}' | tr -d '%')
wget -O ./ci/out/static/coverage.svg "https://img.shields.io/badge/coverage-${percent}%25-success"
