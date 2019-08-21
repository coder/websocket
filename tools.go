// +build tools

package tools

// See https://github.com/go-modules-by-example/index/blob/master/010_tools/README.md
import (
	_ "github.com/jstemmer/go-junit-report"
	_ "go.coder.com/go-tools/cmd/goimports"
	_ "golang.org/x/lint/golint"
	_ "golang.org/x/tools/cmd/stringer"
	_ "mvdan.cc/sh/cmd/shfmt"
)
