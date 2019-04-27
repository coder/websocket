package websocket

import (
	"encoding/binary"
)

// xor applies the WebSocket masking algorithm to p
// with the given key where the first 3 bits of pos
// are the starting position in the key.
// See https://tools.ietf.org/html/rfc6455#section-5.3
//
// The returned value is the position of the next byte
// to be used for masking in the key. This is so that
// unmasking can be performed without the entire frame.
func fastXOR(key [4]byte, keyPos int, b []byte) int {
	// If the payload is greater than 16 bytes, then it's worth
	// masking 8 bytes at a time.
	// Optimization from https://github.com/golang/go/issues/31586#issuecomment-485530859
	if len(b) > 16 {
		// We first create a key that is 8 bytes long
		// and is aligned on the position correctly.
		var alignedKey [8]byte
		for i := range alignedKey {
			alignedKey[i] = key[(i+keyPos)&3]
		}
		k := binary.LittleEndian.Uint64(alignedKey[:])

		// Then we xor until b is less than 8 bytes.
		for len(b) >= 8 {
			v := binary.LittleEndian.Uint64(b)
			binary.LittleEndian.PutUint64(b, v^k)
			b = b[8:]
		}
	}

	// xor remaining bytes.
	for i := range b {
		b[i] ^= key[keyPos&3]
		keyPos++
	}
	return keyPos & 3
}
