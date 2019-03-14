#!/usr/bin/env bash

source .github/lib.sh

go test -race -v -vet=off ./...
