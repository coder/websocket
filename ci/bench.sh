#!/bin/sh
set -eu
cd -- "$(dirname "$0")/.."

go test --run=^$ --bench=. --benchmem --memprofile ci/out/prof.mem --cpuprofile ci/out/prof.cpu -o ci/out/websocket.test "$@" .
(
  cd ./internal/thirdparty
  go test --run=^$ --bench=. --benchmem --memprofile ../../ci/out/prof-thirdparty.mem --cpuprofile ../../ci/out/prof-thirdparty.cpu -o ../../ci/out/thirdparty.test "$@" .

  GOARCH=arm64 go test -c -o ../../ci/out/thirdparty-arm64.test .
  if [ "${CI-}" ]; then
	  sudo apt-get update
	  sudo apt-get install -y qemu-user-static
	  alias qemu-aarch64=qemu-aarch64-static
  fi
  qemu-aarch64 ../../ci/out/thirdparty-arm64.test --test.run=^$ --test.bench=Benchmark_mask --test.benchmem --test.memprofile ../../ci/out/prof-thirdparty-arm64.mem --test.cpuprofile ../../ci/out/prof-thirdparty-arm64.cpu .
)
