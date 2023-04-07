#!/bin/sh
set -eu

cd "$(dirname "$0")"

fmt() {
	go mod tidy
	gofmt -s -w .
	goimports -w "-local=$(go list -m)" .
}

if ! command -v wasmbrowsertest >/dev/null; then
	go install github.com/agnivade/wasmbrowsertest@latest
fi

fmt
go test -race --timeout=1h ./... "$@"
