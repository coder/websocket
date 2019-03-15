#!/usr/bin/env bash

source .github/lib.sh

go test -race -v -coverprofile=coverage.txt -vet=off ./...

bash <(curl -s https://codecov.io/bash)
