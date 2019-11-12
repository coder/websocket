package wsframe_test

import (
	"crypto/rand"
	"encoding/binary"
	"github.com/gobwas/ws"
	"github.com/google/go-cmp/cmp"
	"math/bits"
	"nhooyr.io/websocket/internal/wsframe"
	"strconv"
	"testing"
	_ "unsafe"
)

func Test_mask(t *testing.T) {
	t.Parallel()

	key := []byte{0xa, 0xb, 0xc, 0xff}
	key32 := binary.LittleEndian.Uint32(key)
	p := []byte{0xa, 0xb, 0xc, 0xf2, 0xc}
	gotKey32 := wsframe.Mask(key32, p)

	if exp := []byte{0, 0, 0, 0x0d, 0x6}; !cmp.Equal(exp, p) {
		t.Fatalf("unexpected mask: %v", cmp.Diff(exp, p))
	}

	if exp := bits.RotateLeft32(key32, -8); !cmp.Equal(exp, gotKey32) {
		t.Fatalf("unexpected mask key: %v", cmp.Diff(exp, gotKey32))
	}
}

func basicMask(maskKey [4]byte, pos int, b []byte) int {
	for i := range b {
		b[i] ^= maskKey[pos&3]
		pos++
	}
	return pos & 3
}

//go:linkname gorillaMaskBytes github.com/gorilla/websocket.maskBytes
func gorillaMaskBytes(key [4]byte, pos int, b []byte) int

func Benchmark_mask(b *testing.B) {
	sizes := []int{
		2,
		3,
		4,
		8,
		16,
		32,
		128,
		512,
		4096,
		16384,
	}

	fns := []struct {
		name string
		fn   func(b *testing.B, key [4]byte, p []byte)
	}{
		{
			name: "basic",
			fn: func(b *testing.B, key [4]byte, p []byte) {
				for i := 0; i < b.N; i++ {
					basicMask(key, 0, p)
				}
			},
		},

		{
			name: "nhooyr",
			fn: func(b *testing.B, key [4]byte, p []byte) {
				key32 := binary.LittleEndian.Uint32(key[:])
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					wsframe.Mask(key32, p)
				}
			},
		},
		{
			name: "gorilla",
			fn: func(b *testing.B, key [4]byte, p []byte) {
				for i := 0; i < b.N; i++ {
					gorillaMaskBytes(key, 0, p)
				}
			},
		},
		{
			name: "gobwas",
			fn: func(b *testing.B, key [4]byte, p []byte) {
				for i := 0; i < b.N; i++ {
					ws.Cipher(p, key, 0)
				}
			},
		},
	}

	var key [4]byte
	_, err := rand.Read(key[:])
	if err != nil {
		b.Fatalf("failed to populate mask key: %v", err)
	}

	for _, size := range sizes {
		p := make([]byte, size)

		b.Run(strconv.Itoa(size), func(b *testing.B) {
			for _, fn := range fns {
				b.Run(fn.name, func(b *testing.B) {
					b.SetBytes(int64(size))

					fn.fn(b, key, p)
				})
			}
		})
	}
}
