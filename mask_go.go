//go:build !amd64 && !arm64

package websocket

func mask(b []byte, key uint32) uint32 {
	return maskGo(b, key)
}
