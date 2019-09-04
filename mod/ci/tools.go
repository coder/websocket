// +build tools

package ci

// See https://github.com/go-modules-by-example/index/blob/master/010_tools/README.md
import (
	_ "go.coder.com/go-tools/cmd/goimports"
	_ "golang.org/x/lint/golint"
	_ "golang.org/x/tools/cmd/stringer"
	_ "gotest.tools/gotestsum"
	_ "mvdan.cc/sh/cmd/shfmt"
)
