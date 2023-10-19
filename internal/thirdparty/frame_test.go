package thirdparty

import (
	"encoding/binary"
	"strconv"
	"testing"
	_ "unsafe"

	"github.com/gobwas/ws"
	_ "github.com/gorilla/websocket"

	_ "nhooyr.io/websocket"
)

func basicMask(maskKey [4]byte, pos int, b []byte) int {
	for i := range b {
		b[i] ^= maskKey[pos&3]
		pos++
	}
	return pos & 3
}

//go:linkname gorillaMaskBytes github.com/gorilla/websocket.maskBytes
func gorillaMaskBytes(key [4]byte, pos int, b []byte) int

//go:linkname mask nhooyr.io/websocket.mask
func mask(key32 uint32, b []byte) int

//go:linkname maskGo nhooyr.io/websocket.maskGo
func maskGo(key32 uint32, b []byte) int

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
			name: "nhooyr-go",
			fn: func(b *testing.B, key [4]byte, p []byte) {
				key32 := binary.LittleEndian.Uint32(key[:])
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					maskGo(key32, p)
				}
			},
		},
		{
			name: "wdvxdr1123-asm",
			fn: func(b *testing.B, key [4]byte, p []byte) {
				key32 := binary.LittleEndian.Uint32(key[:])
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					mask(key32, p)
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

	key := [4]byte{1, 2, 3, 4}

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
