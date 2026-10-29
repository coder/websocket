#!/usr/bin/env bash
set -euo pipefail

cd -- "$(dirname "$0")/.."

go install github.com/agnivade/wasmbrowsertest@8be019f6c6dceae821467b4c589eb195c2b761ce

echo "+++ Testing websocket library and generating coverage"
rm -rf ci/out/profile.txt ci/out/profile
coverdir="$(pwd)/ci/out/profile"
mkdir -p "$coverdir"

coverpkgs=$(go list ./... | grep -v websocket/internal/test | paste -sd, -)

echo "++++ Running main tests"
# -test.gocoverdir is a test binary flag, so it goes after -args. Each test
# binary runs in its package directory, so the path must be absolute.
go test -race --bench=. --timeout=1h -cover -covermode=atomic -coverpkg="$coverpkgs" "$@" ./... -args -test.gocoverdir="$coverdir"

echo "+++ Testing examples"
pushd internal/examples
go test "$@" ./...
popd

echo "+++ Testing thirdparty"
pushd internal/thirdparty
go test "$@" ./...
popd

if [[ $# -eq 0 ]]; then
	echo "+++ Running TestMask for arm64"

	run_test=(go test .)
	if [[ $(go env GOARCH) != "arm64" ]]; then
		if [ "${CI-}" ]; then
			sudo apt-get update
			sudo apt-get install -y qemu-user
		fi
		GOARCH=arm64 go test -c -o ./ci/out/websocket-arm64.test "$@" .
		run_test=(qemu-aarch64 ./ci/out/websocket-arm64.test)
	fi
	"${run_test[@]}" -test.run=TestMask
fi

echo "++++ Generating test coverage data"
go tool covdata textfmt -i="$coverdir" -o ci/out/profile.txt
sed -i.bak '/stringer\.go/d' ci/out/profile.txt
go tool cover -func ci/out/profile.txt | tail -n1 # Last line is the total coverage.
go tool cover -html=ci/out/profile.txt -o=ci/out/coverage.html

echo "+++ Done"
