# Contributing

## Issues

Please be as descriptive as possible with your description.

## Pull requests

Please split up changes into several small descriptive commits.

Please capitalize the first word in the commit message title.

The commit message title should use the verb tense + phrase that completes the blank in

> This change modifies websocket to ___________

Be sure to link to an existing issue if one exists. In general, try creating an issue
before making a PR to get some discussion going and to make sure you do not spend time
on a PR that may be rejected.

Run `ci/run.sh` to test your changes. You'll need [shellcheck](https://github.com/koalaman/shellcheck#installing), the [Autobahn Test suite pip package](https://github.com/crossbario/autobahn-testsuite) and Go.

See [../ci/lint.sh](../ci/lint.sh) and [../ci/lint.sh](../ci/test.sh) for the
installation of shellcheck and the Autobahn test suite on Debian or Ubuntu.

For Go, please refer to the [offical docs](https://golang.org/doc/install).

You can benchmark the library with `./ci/benchmark.sh`. You only need Go to run that script.
