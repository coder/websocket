fmt: modtidy gofmt goimports prettier shfmt
ifdef CI
	./ci/ensure_fmt.sh
endif

modtidy: gen
	go mod tidy

gofmt: gen
	gofmt -w -s .

goimports: gen
	goimports -w "-local=$$(go list -m)" .

prettier:
	prettier --write --print-width=120 --no-semi --trailing-comma=all --loglevel=warn --arrow-parens=avoid $$(git ls-files "*.yml" "*.md" "*.js" "*.css" "*.html")

gen:
	stringer -type=opcode,MessageType,StatusCode -output=stringer.go

shfmt:
	shfmt -i 2 -w -s -sr $$(git ls-files "*.sh")
