#!/usr/bin/env bash

set -euxo pipefail

export GO111MODULE=on
export GOFLAGS=-mod=readonly

go test -v -vet=off ./...
