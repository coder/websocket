#!/bin/sh
set -eu
cd -- "$(dirname "$0")/.."

# Pin golang.org/x/tools, the go.mod of v0.25.0 is incompatible with Go 1.19.
X_TOOLS_VERSION=v0.24.0

go mod tidy
(cd ./internal/thirdparty && go mod tidy)
(cd ./internal/examples && go mod tidy)
gofmt -w -s .
go run golang.org/x/tools/cmd/goimports@${X_TOOLS_VERSION} -w "-local=$(go list -m)" .

git ls-files "*.yml" "*.md" "*.js" "*.css" "*.html" | xargs npx prettier@3.3.3 \
  --check \
  --log-level=warn \
  --print-width=90 \
  --no-semi \
  --single-quote \
  --arrow-parens=avoid

go run golang.org/x/tools/cmd/stringer@${X_TOOLS_VERSION} -type=opcode,MessageType,StatusCode -output=stringer.go

if [ "${CI-}" ]; then
  git diff --exit-code
fi
