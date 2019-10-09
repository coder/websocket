all: fmt lint test

.SILENT:

.PHONY: *

include ci/fmt.mk
include ci/lint.mk
include ci/test.mk

ci-image:
	docker build -f ./ci/image/Dockerfile -t nhooyr/websocket-ci .
	docker push nhooyr/websocket-ci
