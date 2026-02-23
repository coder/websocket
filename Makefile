.PHONY: all
all: fmt lint test

.PHONY: fmt
fmt:
	./ci/fmt.sh

.PHONY: lint
lint:
	./ci/lint.sh

.PHONY: test
test:
	./ci/test.sh

.PHONY: static
static:
	make test
	./ci/static.sh

.PHONY: bench
bench:
	./ci/bench.sh
