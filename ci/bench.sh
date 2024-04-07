#!/bin/sh
set -eu
cd -- "$(dirname "$0")/.."

go test --run=^$ --bench=. --benchmem "$@" ./...
# For profiling add: --memprofile ci/out/prof.mem --cpuprofile ci/out/prof.cpu -o ci/out/websocket.test
(
  cd ./internal/thirdparty
  go test --run=^$ --bench=. --benchmem "$@" .

  GOARCH=arm64 go test -c -o ../../ci/out/thirdparty-arm64.test "$@" .
  if [ "$#" -eq 0 ]; then
    if [ "${CI-}" ]; then
      sudo apt-get update
      sudo apt-get install -y qemu-user-static
	  ln -s /usr/bin/qemu-aarch64-static /usr/local/bin/qemu-aarch64
    fi
    qemu-aarch64 ../../ci/out/thirdparty-arm64.test --test.run=^$ --test.bench=Benchmark_mask --test.benchmem
  fi
)
