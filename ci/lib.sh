#!/usr/bin/env bash

set -euo pipefail

if [[ ${CI:-} ]]; then
  export GOFLAGS=-mod=readonly
  export DEBIAN_FRONTEND=noninteractive
fi

cd "$(git rev-parse --show-toplevel)"
