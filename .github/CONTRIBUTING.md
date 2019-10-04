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
It will collect coverage and report it to [codecov](https://codecov.io/gh/nhooyr/websocket)
and also upload a html `coverage` artifact that you can download to browse coverage.

You can run CI locally. You only need [Go](https://golang.org), [nodejs](https://nodejs.org/en/) and [yarn](https://yarnpkg.com).

See the scripts in [package.json](../package.json).

1. `yarn fmt` performs code generation and formatting.
1. `yarn lint` performs linting.
1. `yarn test` runs tests.
1. `yarn all` runs the above scripts in parallel.

For coverage details locally, see `ci/out/coverage.html` after running `yarn test`.

CI is written with nodejs to enable running as much as possible concurrently.

See [ci/image/Dockerfile](../ci/image/Dockerfile) for the installation of the CI dependencies on Ubuntu.

You can also run tests normally with `go test`. `yarn test` just passes a default set of flags to
`go test` to collect coverage and runs the WASM tests.

You can pass flags to `yarn test` if you want to run a specific test or otherwise
control the behaviour of `go test` but also get coverage.

Coverage percentage from codecov and the CI scripts will be different because they are calculated differently.
