package ws

// Mask applies the WebSocket masking algorithm to p
// with the given key where the first 3 bits of pos
// are the starting position in the key.
// See https://tools.ietf.org/html/rfc6455#section-5.3
//
// It is highly optimized to mask per word with the usage
// of unsafe.
//
// For targets that do not support unsafe, please report an issue.
// There is a mask by byte function below that will be used for such targets.
func mask(key [4]byte, pos int, p []byte) int {
	panic("TODO")
}
