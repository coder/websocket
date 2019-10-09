#!/usr/bin/env bash

set -euo pipefail

if [[ $(git ls-files --other --modified --exclude-standard) != "" ]]; then
  echo "Files need generation or are formatted incorrectly."
  git status
  echo "Please run the following locally:"
  echo "  make fmt"
  exit 1
fi
