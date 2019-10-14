test: gotest ci/out/coverage.html
ifdef CI
test: coveralls
endif

ci/out/coverage.html: gotest
	go tool cover -html=ci/out/coverage.prof -o=ci/out/coverage.html

coveralls: gotest
	echo "--- coveralls"
	goveralls -coverprofile=ci/out/coverage.prof -service=github-actions

gotest:
	go test -covermode=count -coverprofile=ci/out/coverage.prof -coverpkg=./... $${GOTESTFLAGS-} ./...
	sed -i '/_stringer\.go/d' ci/out/coverage.prof
	sed -i '/wsecho\.go/d' ci/out/coverage.prof
	sed -i '/assert\.go/d' ci/out/coverage.prof
	sed -i '/wsgrace\.go/d' ci/out/coverage.prof
