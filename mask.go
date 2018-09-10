package ws

// Mask applies the websocket masking algorithm to p
// with the given key where the first 3 bits of pos
// are the starting position in the key.
// See https://tools.ietf.org/html/rfc6455#section-5.3
//
// It is highly optimized to mask per word for targets that
// support the unsafe package.
// For other targets, it masks per byte.
func Mask(key [4]byte, pos int, p []byte) int {
	return mask(key, pos, p)
}

func maskByByte(key [4]byte, pos int, p []byte) int {
	for i := range p {
		p[i] ^= key[pos&3]
		pos++
	}

	return pos
}
