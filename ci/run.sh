#!/usr/bin/env bash

# This script is for local testing. See .github/workflows/ci.yml for CI.

set -euo pipefail
cd "$(dirname "${0}")"
cd "$(git rev-parse --show-toplevel)"

echo "--- fmt"
./ci/fmt.sh

echo "--- lint"
./ci/lint.sh

echo "--- test"
./ci/test.sh

echo "--- wasm"
./ci/wasm.sh
