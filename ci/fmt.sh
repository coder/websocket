#!/bin/sh
set -eu
cd -- "$(dirname "$0")/.."

go mod tidy
(cd ./internal/thirdparty && go mod tidy)
(cd ./internal/examples && go mod tidy)
gofmt -w -s .
go run golang.org/x/tools/cmd/goimports@latest -w "-local=$(go list -m)" .

npx prettier@3.0.3 \
  --write \
  --log-level=warn \
  --print-width=90 \
  --no-semi \
  --single-quote \
  --arrow-parens=avoid \
  $(git ls-files "*.yml" "*.md" "*.js" "*.css" "*.html")

go run golang.org/x/tools/cmd/stringer@latest -type=opcode,MessageType,StatusCode -output=stringer.go

if [ "${CI-}" ]; then
  git diff --exit-code
fi
