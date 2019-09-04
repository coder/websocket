package websocket

import (
	"crypto/rand"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_xor(t *testing.T) {
	t.Parallel()

	key := [4]byte{0xa, 0xb, 0xc, 0xff}
	p := []byte{0xa, 0xb, 0xc, 0xf2, 0xc}
	pos := 0
	pos = fastXOR(key, pos, p)

	if exp := []byte{0, 0, 0, 0x0d, 0x6}; !cmp.Equal(exp, p) {
		t.Fatalf("unexpected mask: %v", cmp.Diff(exp, p))
	}

	if exp := 1; !cmp.Equal(exp, pos) {
		t.Fatalf("unexpected mask pos: %v", cmp.Diff(exp, pos))
	}
}

func basixXOR(maskKey [4]byte, pos int, b []byte) int {
	for i := range b {
		b[i] ^= maskKey[pos&3]
		pos++
	}
	return pos & 3
}

func BenchmarkXOR(b *testing.B) {
	sizes := []int{
		2,
		16,
		32,
		512,
		4096,
		16384,
	}

	fns := []struct {
		name string
		fn   func([4]byte, int, []byte) int
	}{
		{
			"basic",
			basixXOR,
		},
		{
			"fast",
			fastXOR,
		},
	}

	var maskKey [4]byte
	_, err := rand.Read(maskKey[:])
	if err != nil {
		b.Fatalf("failed to populate mask key: %v", err)
	}

	for _, size := range sizes {
		data := make([]byte, size)

		b.Run(strconv.Itoa(size), func(b *testing.B) {
			for _, fn := range fns {
				b.Run(fn.name, func(b *testing.B) {
					b.ReportAllocs()
					b.SetBytes(int64(size))

					for i := 0; i < b.N; i++ {
						fn.fn(maskKey, 0, data)
					}
				})
			}
		})
	}
}
