#!/usr/bin/env bash

# This script is for local testing. See .circleci for CI.

set -euo pipefail
cd "$(dirname "${0}")"
source ./lib.sh

./ci/fmt.sh
./ci/lint.sh
./ci/test.sh
