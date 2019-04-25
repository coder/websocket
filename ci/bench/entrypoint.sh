#!/usr/bin/env bash

source ci/lib.sh || exit 1

mkdir -p profs

go test --vet=off --run=^$ -bench=. \
	-cpuprofile=profs/cpu \
	-memprofile=profs/mem \
	-blockprofile=profs/block \
	-mutexprofile=profs/mutex \
	.

set +x
echo
echo "profiles are in ./profs
keep in mind that every profiler Go provides is enabled so that may skew the benchmarks"
