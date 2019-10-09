lint: govet golint govet-wasm golint-wasm shellcheck

govet:
	go vet ./...

govet-wasm:
	GOOS=js GOARCH=wasm go vet ./...

golint:
	golint -set_exit_status ./...

golint-wasm:
	GOOS=js GOARCH=wasm golint -set_exit_status ./...

shellcheck:
	shellcheck -x $$(git ls-files "*.sh")
