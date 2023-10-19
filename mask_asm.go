//go:build amd64 || arm64

package websocket

func mask(b []byte, key uint32) uint32 {
	if len(b) > 0 {
		return maskAsm(&b[0], len(b), key)
	}
	return key
}

//go:noescape
func maskAsm(b *byte, len int, key uint32) uint32
