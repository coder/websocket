#!/usr/bin/env bash

source .github/lib.sh

go test -v -vet=off ./...
