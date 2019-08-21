#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"
cd "$(git rev-parse --show-toplevel)"

docker build -f ./ci/image/Dockerfile -t nhooyr/websocket-ci .
docker push nhooyr/websocket-ci
