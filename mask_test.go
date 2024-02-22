package websocket

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"math/big"
	"math/bits"
	"testing"

	"nhooyr.io/websocket/internal/test/assert"
)

func basicMask(b []byte, key uint32) uint32 {
	for i := range b {
		b[i] ^= byte(key)
		key = bits.RotateLeft32(key, -8)
	}
	return key
}

func basicMask2(b []byte, key uint32) uint32 {
	keyb := binary.LittleEndian.AppendUint32(nil, key)
	pos := 0
	for i := range b {
		b[i] ^= keyb[pos&3]
		pos++
	}
	return bits.RotateLeft32(key, (pos&3)*-8)
}

func TestMask(t *testing.T) {
	t.Parallel()

	testMask(t, "basicMask", basicMask)
	testMask(t, "maskGo", maskGo)
	testMask(t, "basicMask2", basicMask2)
}

func testMask(t *testing.T, name string, fn func(b []byte, key uint32) uint32) {
	t.Run(name, func(t *testing.T) {
		t.Parallel()
		for i := 0; i < 9999; i++ {
			keyb := make([]byte, 4)
			_, err := rand.Read(keyb)
			assert.Success(t, err)
			key := binary.LittleEndian.Uint32(keyb)

			n, err := rand.Int(rand.Reader, big.NewInt(1<<16))
			assert.Success(t, err)

			b := make([]byte, 1+n.Int64())
			_, err = rand.Read(b)
			assert.Success(t, err)

			b2 := make([]byte, len(b))
			copy(b2, b)
			b3 := make([]byte, len(b))
			copy(b3, b)

			key2 := basicMask(b2, key)
			key3 := fn(b3, key)

			if key2 != key3 {
				t.Errorf("expected key %X but got %X", key2, key3)
			}
			if !bytes.Equal(b2, b3) {
				t.Error("bad bytes")
				return
			}
		}
	})
}
