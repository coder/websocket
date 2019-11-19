package errd

import (
	"fmt"
)

func Wrap(err *error, f string, v ...interface{}) {
	if *err != nil {
		*err = fmt.Errorf(f+ ": %w", append(v, *err)...)
	}
}
