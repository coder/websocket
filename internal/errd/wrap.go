package errd

import (
	"fmt"

	"golang.org/x/xerrors"
)

type wrapError struct {
	msg   string
	err   error
	frame xerrors.Frame
}

func (e *wrapError) Error() string {
	return fmt.Sprint(e)
}

func (e *wrapError) Format(s fmt.State, v rune) { xerrors.FormatError(e, s, v) }

func (e *wrapError) FormatError(p xerrors.Printer) (next error) {
	p.Print(e.msg)
	e.frame.Format(p)
	return e.err
}

func (e *wrapError) Unwrap() error {
	return e.err
}

// Wrap wraps err with xerrors.Errorf if err is non nil.
// Intended for use with defer and a named error return.
// Inspired by https://github.com/golang/go/issues/32676.
func Wrap(err *error, f string, v ...interface{}) {
	if *err != nil {
		*err = &wrapError{
			msg:   fmt.Sprintf(f, v...),
			err:   *err,
			frame: xerrors.Caller(1),
		}
	}
}
