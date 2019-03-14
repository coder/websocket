#!/usr/bin/env bash

source .github/lib.sh

go vet -composites=false ./...
