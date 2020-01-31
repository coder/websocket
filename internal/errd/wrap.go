package errd

import "golang.org/x/xerrors"

// Wrap wraps err with xerrors.Errorf if err is non nil.
// Intended for use with defer and a named error return.
// Inspired by https://github.com/golang/go/issues/32676.
func Wrap(err *error, f string, v ...interface{}) {
	if *err != nil {
		*err = xerrors.Errorf(f+": %w", append(v, *err)...)
	}
}
