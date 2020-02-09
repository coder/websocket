package xsync

import (
	"testing"

	"nhooyr.io/websocket/internal/test/cmp"
)

func TestGoRecover(t *testing.T) {
	t.Parallel()

	errs := Go(func() error {
		panic("anmol")
	})

	err := <-errs
	if !cmp.ErrorContains(err, "anmol") {
		t.Fatalf("unexpected err: %v", err)
	}
}
