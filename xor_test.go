package websocket

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_xor(t *testing.T) {
	t.Parallel()

	key := [4]byte{0xa, 0xb, 0xc, 0xff}
	p := []byte{0xa, 0xb, 0xc, 0xf2, 0xc}
	pos := 0
	pos = xor(key, pos, p)

	if exp := []byte{0, 0, 0, 0x0d, 0x6}; !cmp.Equal(exp, p) {
		t.Fatalf("unexpected mask: %v", cmp.Diff(exp, p))
	}

	if exp := 1; !cmp.Equal(exp, pos) {
		t.Fatalf("unexpected mask pos: %v", cmp.Diff(exp, pos))
	}
}
