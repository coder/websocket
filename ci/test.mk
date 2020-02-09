test: gotest ci/out/coverage.html
ifdef CI
test: coveralls
endif

ci/out/coverage.html: gotest
	go tool cover -html=ci/out/coverage.prof -o=ci/out/coverage.html

coveralls: gotest
	# https://github.com/coverallsapp/github-action/blob/master/src/run.ts
	echo "--- coveralls"
	goveralls -coverprofile=ci/out/coverage.prof

gotest:
	go test -covermode=count -coverprofile=ci/out/coverage.prof -coverpkg=./... $${GOTESTFLAGS-} ./...
	sed -i '/stringer\.go/d' ci/out/coverage.prof
	sed -i '/nhooyr.io\/websocket\/internal\/test/d' ci/out/coverage.prof
