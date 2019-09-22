// +build !js

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
	// If the payload is greater than or equal to 16 bytes, then it's worth
	// masking 8 bytes at a time.
	// Optimization from https://github.com/golang/go/issues/31586#issuecomment-485530859
	if len(b) >= 16 {
		// We first create a key that is 8 bytes long
		// and is aligned on the position correctly.
		var alignedKey [8]byte
		for i := range alignedKey {
			alignedKey[i] = key[(i+keyPos)&3]
		}
		k := binary.LittleEndian.Uint64(alignedKey[:])

		// At some point in the future we can clean these unrolled loops up.
		// See https://github.com/golang/go/issues/31586#issuecomment-487436401

		// Then we xor until b is less than 128 bytes.
		for len(b) >= 128 {
			v := binary.LittleEndian.Uint64(b)
			binary.LittleEndian.PutUint64(b, v^k)
			v = binary.LittleEndian.Uint64(b[8:])
			binary.LittleEndian.PutUint64(b[8:], v^k)
			v = binary.LittleEndian.Uint64(b[16:])
			binary.LittleEndian.PutUint64(b[16:], v^k)
			v = binary.LittleEndian.Uint64(b[24:])
			binary.LittleEndian.PutUint64(b[24:], v^k)
			v = binary.LittleEndian.Uint64(b[32:])
			binary.LittleEndian.PutUint64(b[32:], v^k)
			v = binary.LittleEndian.Uint64(b[40:])
			binary.LittleEndian.PutUint64(b[40:], v^k)
			v = binary.LittleEndian.Uint64(b[48:])
			binary.LittleEndian.PutUint64(b[48:], v^k)
			v = binary.LittleEndian.Uint64(b[56:])
			binary.LittleEndian.PutUint64(b[56:], v^k)
			v = binary.LittleEndian.Uint64(b[64:])
			binary.LittleEndian.PutUint64(b[64:], v^k)
			v = binary.LittleEndian.Uint64(b[72:])
			binary.LittleEndian.PutUint64(b[72:], v^k)
			v = binary.LittleEndian.Uint64(b[80:])
			binary.LittleEndian.PutUint64(b[80:], v^k)
			v = binary.LittleEndian.Uint64(b[88:])
			binary.LittleEndian.PutUint64(b[88:], v^k)
			v = binary.LittleEndian.Uint64(b[96:])
			binary.LittleEndian.PutUint64(b[96:], v^k)
			v = binary.LittleEndian.Uint64(b[104:])
			binary.LittleEndian.PutUint64(b[104:], v^k)
			v = binary.LittleEndian.Uint64(b[112:])
			binary.LittleEndian.PutUint64(b[112:], v^k)
			v = binary.LittleEndian.Uint64(b[120:])
			binary.LittleEndian.PutUint64(b[120:], v^k)
			b = b[128:]
		}

		// Then we xor until b is less than 64 bytes.
		for len(b) >= 64 {
			v := binary.LittleEndian.Uint64(b)
			binary.LittleEndian.PutUint64(b, v^k)
			v = binary.LittleEndian.Uint64(b[8:])
			binary.LittleEndian.PutUint64(b[8:], v^k)
			v = binary.LittleEndian.Uint64(b[16:])
			binary.LittleEndian.PutUint64(b[16:], v^k)
			v = binary.LittleEndian.Uint64(b[24:])
			binary.LittleEndian.PutUint64(b[24:], v^k)
			v = binary.LittleEndian.Uint64(b[32:])
			binary.LittleEndian.PutUint64(b[32:], v^k)
			v = binary.LittleEndian.Uint64(b[40:])
			binary.LittleEndian.PutUint64(b[40:], v^k)
			v = binary.LittleEndian.Uint64(b[48:])
			binary.LittleEndian.PutUint64(b[48:], v^k)
			v = binary.LittleEndian.Uint64(b[56:])
			binary.LittleEndian.PutUint64(b[56:], v^k)
			b = b[64:]
		}

		// Then we xor until b is less than 32 bytes.
		for len(b) >= 32 {
			v := binary.LittleEndian.Uint64(b)
			binary.LittleEndian.PutUint64(b, v^k)
			v = binary.LittleEndian.Uint64(b[8:])
			binary.LittleEndian.PutUint64(b[8:], v^k)
			v = binary.LittleEndian.Uint64(b[16:])
			binary.LittleEndian.PutUint64(b[16:], v^k)
			v = binary.LittleEndian.Uint64(b[24:])
			binary.LittleEndian.PutUint64(b[24:], v^k)
			b = b[32:]
		}

		// Then we xor until b is less than 16 bytes.
		for len(b) >= 16 {
			v := binary.LittleEndian.Uint64(b)
			binary.LittleEndian.PutUint64(b, v^k)
			v = binary.LittleEndian.Uint64(b[8:])
			binary.LittleEndian.PutUint64(b[8:], v^k)
			b = b[16:]
		}

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
