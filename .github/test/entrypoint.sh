#!/usr/bin/env bash

source .github/lib.sh

# Unfortunately, this is the only way to ensure go.mod and go.sum are correct.
# See https://github.com/golang/go/issues/27005
go list ./... > /dev/null
go mod tidy

go install golang.org/x/tools/cmd/stringer
go generate ./...

if [[ $CI ]] && unstaged_files; then
	set +x
	echo
	echo "generated code needs an update"
	echo "please run:"
	echo "./test.sh"
	echo
	git status
	exit 1
fi

(
	shopt -s globstar nullglob dotglob
	shellcheck ./**/*.sh
)

go vet -composites=false ./...

COVERAGE_PROFILE=$(mktemp)
go test -race -v "-coverprofile=${COVERAGE_PROFILE}" -vet=off ./...
go tool cover "-func=${COVERAGE_PROFILE}"

if [[ $CI ]]; then
	bash <(curl -s https://codecov.io/bash)
else
	go tool cover "-html=${COVERAGE_PROFILE}" -o=coverage.html
fi
