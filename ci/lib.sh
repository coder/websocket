#!/usr/bin/env bash

set -euo pipefail

# Ensures $CI can be used if it's set or not.
export CI=${CI:-}
if [[ $CI ]]; then
  export GOFLAGS=-mod=readonly
  export DEBIAN_FRONTEND=noninteractive
fi

cd "$(git rev-parse --show-toplevel)"
