#!/usr/bin/env bash

source .github/lib.sh

go vet -composites=false ./...

mapfile -t scripts <<< "$(find . -type f -name "*.sh")"
shellcheck "${scripts[@]}"
