#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname "${0}")"

docker build -t nhooyr/websocket-ci .
docker push nhooyr/websocket-ci
