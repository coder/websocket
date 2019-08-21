#!/usr/bin/env bash

# This script is for local testing. See .circleci for CI.

set -euo pipefail
cd "$(dirname "${0}")"
cd "$(git rev-parse --show-toplevel)"

./ci/fmt.sh
./ci/lint.sh
./ci/test.sh
