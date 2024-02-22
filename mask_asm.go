//go:build amd64 || arm64

package websocket

func mask(b []byte, key uint32) uint32 {
	if len(b) > 0 {
		return maskAsm(&b[0], len(b), key)
	}
	return key
}

// @nhooyr: I am not confident that the amd64 or the arm64 implementations of this
// function are perfect. There are almost certainly missing optimizations or
// opportunities for simplification. I'm confident there are no bugs though.
// For example, the arm64 implementation doesn't align memory like the amd64.
// Or the amd64 implementation could use AVX512 instead of just AVX2.
// The AVX2 code I had to disable anyway as it wasn't performing as expected.
// See https://github.com/nhooyr/websocket/pull/326#issuecomment-1771138049
//
//go:noescape
func maskAsm(b *byte, len int, key uint32) uint32
