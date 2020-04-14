test: ci/out/coverage.html
ifdef CI
test: coveralls
endif

ci/out/coverage.html: gotest
	go tool cover -html=ci/out/coverage.prof -o=ci/out/coverage.html

coveralls: gotest
	echo "--- coveralls"
	goveralls -coverprofile=ci/out/coverage.prof

gotest:
	go test -timeout=30m -covermode=atomic -coverprofile=ci/out/coverage.prof -coverpkg=./... $${GOTESTFLAGS-} ./...
	sed -i '/stringer\.go/d' ci/out/coverage.prof
	sed -i '/nhooyr.io\/websocket\/internal\/test/d' ci/out/coverage.prof
	sed -i '/examples/d' ci/out/coverage.prof
