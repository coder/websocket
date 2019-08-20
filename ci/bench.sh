#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
cd "$(git rev-parse --show-toplevel)"

mkdir -p ci/out
go test -vet=off -run=^$ -bench=. -o=ci/out/websocket.test \
  -cpuprofile=ci/out/cpu.prof \
  -memprofile=ci/out/mem.prof \
  -blockprofile=ci/out/block.prof \
  -mutexprofile=ci/out/mutex.prof \
  .

echo
echo "Profiles are in ./ci/out/*.prof
Keep in mind that every profiler Go provides is enabled so that may skew the benchmarks."
