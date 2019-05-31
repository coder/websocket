#!/usr/bin/env bash

source ci/lib.sh || exit 1

go test --vet=off --run=^$ -bench=. -o=ci/out/websocket.test \
	-cpuprofile=ci/out/cpu.prof \
	-memprofile=ci/out/mem.prof \
	-blockprofile=ci/out/block.prof \
	-mutexprofile=ci/out/mutex.prof \
	.

set +x
echo
echo "profiles are in ./ci/out/*.prof
keep in mind that every profiler Go provides is enabled so that may skew the benchmarks"
