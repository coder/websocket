#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
source ./lib.sh

go test -vet=off -run=^$ -bench=. -o=ci/out/websocket.test \
  -cpuprofile=ci/out/cpu.prof \
  -memprofile=ci/out/mem.prof \
  -blockprofile=ci/out/block.prof \
  -mutexprofile=ci/out/mutex.prof \
  .

echo
echo "Profiles are in ./ci/out/*.prof
Keep in mind that every profiler Go provides is enabled so that may skew the benchmarks."
