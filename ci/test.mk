test: gotest gotest-wasm

gotest: _gotest htmlcov
ifdef CI
gotest: codecov
endif

htmlcov: _gotest
	go tool cover -html=ci/out/coverage.prof -o=ci/out/coverage.html

codecov: _gotest
	curl -s https://codecov.io/bash | bash -s -- -Z -f ci/out/coverage.prof

_gotest:
	echo "--- gotest" && go test -parallel=32 -coverprofile=ci/out/coverage.prof -coverpkg=./... $$TESTFLAGS ./...
	sed -i '/_stringer\.go/d' ci/out/coverage.prof
	sed -i '/wsjstest\/main\.go/d' ci/out/coverage.prof
	sed -i '/wsecho\.go/d' ci/out/coverage.prof
	sed -i '/assert\.go/d' ci/out/coverage.prof
	sed -i '/wsgrace\.go/d' ci/out/coverage.prof

gotest-wasm: wsjstest
	echo "--- wsjstest" && ./ci/wasmtest.sh

wsjstest:
	go install ./internal/wsjstest
