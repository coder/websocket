package thirdparty

import (
	"encoding/binary"
	"runtime"
	"strconv"
	"testing"
	_ "unsafe"

	"github.com/gobwas/ws"
	_ "github.com/gorilla/websocket"
	_ "github.com/lesismal/nbio/nbhttp/websocket"

	_ "github.com/coder/websocket"
)

func basicMask(b []byte, maskKey [4]byte, pos int) int {
	for i := range b {
		b[i] ^= maskKey[pos&3]
		pos++
	}
	return pos & 3
}

//go:linkname maskGo github.com/coder/websocket.maskGo
func maskGo(b []byte, key32 uint32) int

//go:linkname maskAsm github.com/coder/websocket.maskAsm
func maskAsm(b *byte, len int, key32 uint32) uint32

//go:linkname nbioMaskBytes github.com/lesismal/nbio/nbhttp/websocket.maskXOR
func nbioMaskBytes(b, key []byte) int

//go:linkname gorillaMaskBytes github.com/gorilla/websocket.maskBytes
func gorillaMaskBytes(key [4]byte, pos int, b []byte) int

func Benchmark_mask(b *testing.B) {
	b.Run(runtime.GOARCH, benchmark_mask)
}

func benchmark_mask(b *testing.B) {
	sizes := []int{
		8,
		16,
		32,
		128,
		256,
		512,
		1024,
		2048,
		4096,
		8192,
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
					basicMask(p, key, 0)
				}
			},
		},

		{
			name: "nhooyr-go",
			fn: func(b *testing.B, key [4]byte, p []byte) {
				key32 := binary.LittleEndian.Uint32(key[:])
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					maskGo(p, key32)
				}
			},
		},
		{
			name: "wdvxdr1123-asm",
			fn: func(b *testing.B, key [4]byte, p []byte) {
				key32 := binary.LittleEndian.Uint32(key[:])
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					maskAsm(&p[0], len(p), key32)
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
		{
			name: "nbio",
			fn: func(b *testing.B, key [4]byte, p []byte) {
				keyb := key[:]
				for i := 0; i < b.N; i++ {
					nbioMaskBytes(p, keyb)
				}
			},
		},
	}

	key := [4]byte{1, 2, 3, 4}

	for _, fn := range fns {
		b.Run(fn.name, func(b *testing.B) {
			for _, size := range sizes {
				p := make([]byte, size)

				b.Run(strconv.Itoa(size), func(b *testing.B) {
					b.SetBytes(int64(size))

					fn.fn(b, key, p)
				})
			}
		})
	}
}
