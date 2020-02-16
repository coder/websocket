#!/usr/bin/env bash

set -euo pipefail

main() {
  local files
  mapfile -t files < <(git ls-files --other --modified --exclude-standard)
  if [[ ${files[*]} == "" ]]; then
    return
  fi

  echo "Files need generation or are formatted incorrectly:"
  for f in "${files[@]}"; do
    echo "  $f"
  done

  echo
  echo "Please run the following locally:"
  echo "  make fmt"
  exit 1
}

main "$@"
