test: gotest ci/out/coverage.html
ifdef CI
test: coveralls
endif

ci/out/coverage.html: gotest
	go tool cover -html=ci/out/coverage.prof -o=ci/out/coverage.html

coveralls: gotest
	# https://github.com/coverallsapp/github-action/blob/master/src/run.ts
	echo "--- coveralls"
	export GIT_BRANCH="$$GITHUB_REF"
	export BUILD_NUMBER="$$GITHUB_SHA"
	if [[ $$GITHUB_EVENT_NAME == pull_request ]]; then
	  export CI_PULL_REQUEST="$$(jq .number "$$GITHUB_EVENT_PATH")"
	  BUILD_NUMBER="$$BUILD_NUMBER-PR-$$CI_PULL_REQUEST"
	fi
	goveralls -coverprofile=ci/out/coverage.prof -service=github

gotest:
	go test -covermode=count -coverprofile=ci/out/coverage.prof -coverpkg=./... $${GOTESTFLAGS-} ./...
	sed -i '/stringer\.go/d' ci/out/coverage.prof
	sed -i '/assert/d' ci/out/coverage.prof
