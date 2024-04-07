//go:build !js
// +build !js

package websocket

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"math/bits"
	"math/rand"
	"strconv"
	"testing"
	"time"

	"nhooyr.io/websocket/internal/test/assert"
)

func TestHeader(t *testing.T) {
	t.Parallel()

	t.Run("lengths", func(t *testing.T) {
		t.Parallel()

		lengths := []int{
			124,
			125,
			126,
			127,

			65534,
			65535,
			65536,
			65537,
		}

		for _, n := range lengths {
			n := n
			t.Run(strconv.Itoa(n), func(t *testing.T) {
				t.Parallel()

				testHeader(t, header{
					payloadLength: int64(n),
				})
			})
		}
	})

	t.Run("fuzz", func(t *testing.T) {
		t.Parallel()

		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		randBool := func() bool {
			return r.Intn(2) == 0
		}

		for i := 0; i < 10000; i++ {
			h := header{
				fin:    randBool(),
				rsv1:   randBool(),
				rsv2:   randBool(),
				rsv3:   randBool(),
				opcode: opcode(r.Intn(16)),

				masked:        randBool(),
				payloadLength: r.Int63(),
			}
			if h.masked {
				h.maskKey = r.Uint32()
			}

			testHeader(t, h)
		}
	})
}

func testHeader(t *testing.T, h header) {
	b := &bytes.Buffer{}
	w := bufio.NewWriter(b)
	r := bufio.NewReader(b)

	err := writeFrameHeader(h, w, make([]byte, 8))
	assert.Success(t, err)

	err = w.Flush()
	assert.Success(t, err)

	h2, err := readFrameHeader(r, make([]byte, 8))
	assert.Success(t, err)

	assert.Equal(t, "read header", h, h2)
}

func Test_mask(t *testing.T) {
	t.Parallel()

	key := []byte{0xa, 0xb, 0xc, 0xff}
	key32 := binary.LittleEndian.Uint32(key)
	p := []byte{0xa, 0xb, 0xc, 0xf2, 0xc}
	gotKey32 := mask(p, key32)

	expP := []byte{0, 0, 0, 0x0d, 0x6}
	assert.Equal(t, "p", expP, p)

	expKey32 := bits.RotateLeft32(key32, -8)
	assert.Equal(t, "key32", expKey32, gotKey32)
}
