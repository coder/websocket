//go:build !appengine && (amd64 || arm64)
// +build !appengine
// +build amd64 arm64

package websocket

import "golang.org/x/sys/cpu"

func mask(key uint32, b []byte) uint32 {
	if len(b) > 0 {
		return maskAsm(&b[0], len(b), key)
	}
	return key
}

var useAVX2 = cpu.X86.HasAVX2

//go:noescape
func maskAsm(b *byte, len int, key uint32) uint32
