fmt: modtidy gofmt goimports prettier
ifdef CI
	./ci/fmtcheck.sh
endif

modtidy: gen
	go mod tidy

gofmt: gen
	gofmt -w -s .

goimports: gen
	goimports -w "-local=$$(go list -m)" .

prettier: gen
	prettier --write --print-width=120 --no-semi --trailing-comma=all --loglevel=warn $$(git ls-files "*.yaml" "*.yml" "*.md" "*.ts")

shfmt: gen
	shfmt -i 2 -w -s -sr .

gen:
	go generate ./...
