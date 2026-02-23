#!/usr/bin/env bash
set -eu

cd -- "$(dirname "$0")/.."

go install github.com/agnivade/wasmbrowsertest@8be019f6c6dceae821467b4c589eb195c2b761ce

echo "+++ Testing websocket library and generating coverage"
# HTTP/2 tests are kept in a separate module currently.
echo "++++ Building HTTP/2 tests"
rm -f ci/out/http2.test
pushd internal/thirdparty
go test -c -cover -covermode=atomic -coverpkg=github.com/coder/websocket -o ../../ci/out/http2.test ./http2
popd

# Run main tests and generate coverage data.
rm -rf ci/out/profile.txt ci/out/profile
coverbase="$(pwd)/ci/out/profile"
coverdirs=("$coverbase"/{xconnect=0,xconnect=1,race=0,race=1})
mkdir -p "${coverdirs[@]}"

# Generate test coverage for http2 codepaths.
echo "++++ Running HTTP/2 tests: http2xconnect=0"
GODEBUG=http2xconnect=0 ./ci/out/http2.test -test.timeout=30s -test.gocoverdir="$coverbase/xconnect=0" -test.run='.*XCONNECT_Disabled$' -test.count=1
echo "++++ Running HTTP/2 tests: http2xconnect=1"
GODEBUG=http2xconnect=1 ./ci/out/http2.test -test.timeout=30s -test.gocoverdir="$coverbase/xconnect=1" -test.run='.*XCONNECT_Enabled$' -test.count=1

coverpkgs=($(go list ./... | grep -v websocket/internal/test))
coverpkgsjoined=$(IFS=,; echo "${coverpkgs[*]}")

echo "++++ Running main tests"
go test --bench=. --timeout=1h -cover -covermode=atomic -coverpkg="$coverpkgsjoined" -test.gocoverdir="$coverbase/race=0" "$@" ./...

echo "++++ Running main tests (race)"
# Disable autobahn for race tests.
AUTOBAHN= go test -race -timeout=1h -cover -covermode=atomic -coverpkg="$coverpkgsjoined" -test.gocoverdir="$coverbase/race=1" "$@" ./...

echo "+++ Testing examples"
pushd internal/examples
go test "$@" ./...
popd

echo "+++ Testing thirdparty"
pushd internal/thirdparty
go test "$@" $(go list ./... | grep -v ./http2)
popd

if [[ $# -eq 0 ]]; then
	echo "+++ Running TestMask for arm64"

	run_test=(go test .)
	if [[ $(go env GOARCH) != "arm64" ]]; then
		if [ "${CI-}" ]; then
			sudo apt-get update
			sudo apt-get install -y qemu-user-static
			ln -s /usr/bin/qemu-aarch64-static /usr/local/bin/qemu-aarch64
		fi
		GOARCH=arm64 go test -c -o ./ci/out/websocket-arm64.test "$@" .
		run_test=(qemu-aarch64 ./ci/out/websocket-arm64.test)
	fi
	"${run_test[@]}" -test.run=TestMask
fi

echo "++++ Generating test coverage data"
mkdir -p "$coverbase/merged"
coverdirsjoined=$(IFS=,; echo "${coverdirs[*]}")
go tool covdata merge -i="$coverdirsjoined" -o "${coverbase}/merged"
go tool covdata textfmt -i="${coverbase}/merged" -o ci/out/profile.txt
go tool cover -func ci/out/profile.txt | tail -n1 # Last line is the total coverage.
go tool cover -html=ci/out/profile.txt -o=ci/out/coverage.html

echo "+++ Done"
exit 0
