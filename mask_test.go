package ws

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"testing"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func TestMask(t *testing.T) {
	t.Parallel()

	// Example 2 from https://tools.ietf.org/html/rfc6455#section-5.7

	key := [4]byte{
		0x37,
		0xfa,
		0x21,
		0x3d,
	}

	p := []byte{
		0x7f,
		0x9f,
		0x4d,
		0x51,
		0x58,
	}

	pos := Mask(key, 0, p)

	if exp := "Hello"; exp != string(p) {
		t.Errorf("expected %q; got %q", exp, string(p))
	}

	if exp := len(p); exp != pos {
		t.Errorf("expected %q; got %q", exp, pos)
	}
}

// TestMask makes heavy use of the unsafe package and so
// heavily fuzzy testing it is necessary to ensure safety.
func TestMaskFuzzy(t *testing.T) {
	t.Parallel()

	for i := 0; i < 4096; i++ {
		t.Run("", func(t *testing.T) {
			t.Parallel()

			key := genKey()
			p := genBytes(128)
			pos := rand.Intn(len(key))

			p2 := copyBytes(p)
			order := rand.Intn(2) == 0

			tc := map[string]interface{}{
				"key":   key,
				"p":     p,
				"pos":   pos,
				"p2":    p2,
				"order": order,
			}

			mask := func() int {
				if order {
					return Mask(key, pos, p2)
				}

				// maskByByte is relatively simple so its much easier to trust.
				// Using it here will catch whether Mask's behaviour matches maskByByte.
				return maskByByte(key, pos, p2)
			}

			expPos := (pos + len(p2)) & 3

			rpos := mask()
			if rpos&3 != expPos {
				t.Fatalf("incorrect pos from mask: tc: %#v; pos: %#v; exp: %#v", tc, rpos&3, expPos)
			}

			rpos = mask()
			if rpos&3 != expPos {
				t.Fatalf("incorrect pos from mask: tc: %#v; pos: %#v; exp: %#v", tc, rpos&3, expPos)
			}

			if !bytes.Equal(p, p2) {
				t.Errorf("origin and double masked should be equal: tc: %#v", tc)
			}
		})
	}
}

func copyBytes(p []byte) []byte {
	var p2 []byte
	p2 = append(p2, p...)

	if p2 == nil && p != nil {
		// p was originally an empty byte slice, not nil.
		p2 = []byte{}
	}

	return p2
}

// This function constructs every possible permutation
// of a byte slice for a given max length.
func genBytes(max int) []byte {
	n := rand.Intn(max + 1)
	if n == 0 {
		return nil
	}

	b := make([]byte, n)

	i1 := rand.Intn(len(b) + 1)
	b = b[i1:]

	if len(b) == 0 {
		// Notable difference from first return, this returns a byte slice
		// without any elements instead of just nil.
		return b
	}

	i2 := rand.Intn(len(b) + 1)
	b = b[:i2]

	return b
}

func genKey() [4]byte {
	var b [4]byte
	key := rand.Uint32()
	binary.BigEndian.PutUint32(b[:], key)
	return b
}
