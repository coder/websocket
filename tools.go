// +build tools

package tools

// See https://github.com/go-modules-by-example/index/blob/master/010_tools/README.md
import (
	_ "golang.org/x/tools/cmd/goimports"
	_ "mvdan.cc/sh/cmd/shfmt"
)
