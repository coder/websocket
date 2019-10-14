# Contributing

## Issues

Please be as descriptive as possible.

Reproducible examples are key to finding and fixing bugs.

## Pull requests

Good issues for first time contributors are marked as such. Feel free to
reach out for clarification on what needs to be done.

Split up large changes into several small descriptive commits.

Capitalize the first word in the commit message title.

The commit message title should use the verb tense + phrase that completes the blank in

> This change modifies websocket to \_\_\_\_\_\_\_\_\_

Be sure to [correctly link](https://help.github.com/en/articles/closing-issues-using-keywords)
to an existing issue if one exists. In general, create an issue before a PR to get some
discussion going and to make sure you do not spend time on a PR that may be rejected.

CI must pass on your changes for them to be merged.

### CI

CI will ensure your code is formatted, lints and passes tests.
It will collect coverage and report it to [coveralls](https://coveralls.io/github/nhooyr/websocket)
and also upload a html `coverage` artifact that you can download to browse coverage.

You can run CI locally.

See [ci/image/Dockerfile](../ci/image/Dockerfile) for the installation of the CI dependencies on Ubuntu.

1. `make fmt` performs code generation and formatting.
1. `make lint` performs linting.
1. `make test` runs tests.
1. `make` runs the above targets.

For coverage details locally, see `ci/out/coverage.html` after running `make test`.

You can run tests normally with `go test`. `make test` wraps around `go test` to collect coverage.
