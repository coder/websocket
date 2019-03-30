package websocket

// mask applies the WebSocket masking algorithm to p
// with the given key where the first 3 bits of pos
// are the starting position in the key.
// See https://tools.ietf.org/html/rfc6455#section-5.3
//
// The returned value is the position of the next byte
// to be used for masking in the key. This is so that
// unmasking can be performed without the entire frame.
func mask(key [4]byte, pos int, p []byte) int {
	for i := range p {
		p[i] ^= key[pos&3]
		pos++
	}
	return pos & 3
}
