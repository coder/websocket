#!/bin/sh
set -eu
cd -- "$(dirname "$0")"

./ci/fmt.sh
./ci/lint.sh
./ci/test.sh
