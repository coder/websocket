#!/usr/bin/env bash

# This script is for local testing. See .github/workflows/ci.yml for CI.

set -euo pipefail
cd "$(dirname "${0}")"
cd "$(git rev-parse --show-toplevel)"

./ci/fmt.sh
./ci/lint.sh
./ci/test.sh
./ci/wasm.sh
