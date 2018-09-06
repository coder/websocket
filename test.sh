#!/usr/bin/env bash
# This is for local testing. See .github for CI.

source ./.github/lib.sh

./.github/fmt/entrypoint.sh
./.github/test/entrypoint.sh
