#!/usr/bin/env bash

set -euxo pipefail

export GO111MODULE=on
export GOFLAGS=-mod=readonly

go vet -composites=false ./...
