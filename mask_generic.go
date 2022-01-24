//go:build appengine || (!amd64 && !arm64 && !js)

package websocket

func mask(key uint32, b []byte) uint32 {
	return maskGo(key, b)
}
