# Contributing

## Issues

Please be as descriptive as possible.

Reproducible examples are key to finding and fixing bugs.

## Pull requests

Good issues for first time contributors are marked as such. Please feel free to
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

CI will ensure your code is formatted correctly, passes linting and tests.
It will collect coverage and report it to [codecov](https://codecov.io/gh/nhooyr/websocket)
and also upload a `out/coverage.html` artifact that you can click on to interactively
browse coverage.

You can run CI locally. The various steps are located in [ci/\*.sh](../ci).

1. [fmt.sh](../ci/fmt.sh) which requires node (specifically prettier).
1. [lint.sh](../ci/lint.sh) which requires [shellcheck](https://github.com/koalaman/shellcheck#installing).
1. [test.sh](../ci/test.sh)
1. [run.sh](../ci/run.sh) which runs the above scripts in order.

For coverage details locally, please see `ci/out/coverage.html` after running [test.sh](../ci/test.sh).

See [ci/image/Dockerfile](../ci/image/Dockerfile) for the installation of the CI dependencies on Ubuntu.

You can also run tests normally with `go test`. [test.sh](../ci/test.sh) just passes a default set of flags to
`go test` to collect coverage and also prettify the output.

You can pass flags to [test.sh](ci/test.sh) if you want to run a specific test or otherwise
control the behaviour of `go test` but also get coverage.

Coverage percentage from codecov and the CI scripts will be different because they are calculated differently.
