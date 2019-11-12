// +build !js

package wsframe

import (
	"bytes"
	"io"
	"math/rand"
	"strconv"
	"testing"
	"time"
	_ "unsafe"

	"github.com/google/go-cmp/cmp"
	_ "github.com/gorilla/websocket"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func randBool() bool {
	return rand.Intn(1) == 0
}

func TestHeader(t *testing.T) {
	t.Parallel()

	t.Run("eof", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name  string
			bytes []byte
		}{
			{
				"start",
				[]byte{0xff},
			},
			{
				"middle",
				[]byte{0xff, 0xff, 0xff},
			},
		}
		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				b := bytes.NewBuffer(tc.bytes)
				_, err := ReadHeader(nil, b)
				if io.ErrUnexpectedEOF != err {
					t.Fatalf("expected %v but got: %v", io.ErrUnexpectedEOF, err)
				}
			})
		}
	})

	t.Run("writeNegativeLength", func(t *testing.T) {
		t.Parallel()

		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("failed to induce panic in writeHeader with negative payload length")
			}
		}()

		Header{
			PayloadLength: -1,
		}.Bytes(nil)
	})

	t.Run("readNegativeLength", func(t *testing.T) {
		t.Parallel()

		b := Header{
			PayloadLength: 1<<16 + 1,
		}.Bytes(nil)

		// Make length negative
		b[2] |= 1 << 7

		r := bytes.NewReader(b)
		_, err := ReadHeader(nil, r)
		if err == nil {
			t.Fatalf("unexpected error value: %+v", err)
		}
	})

	t.Run("lengths", func(t *testing.T) {
		t.Parallel()

		lengths := []int{
			124,
			125,
			126,
			4096,
			16384,
			65535,
			65536,
			65537,
			131072,
		}

		for _, n := range lengths {
			n := n
			t.Run(strconv.Itoa(n), func(t *testing.T) {
				t.Parallel()

				testHeader(t, Header{
					PayloadLength: int64(n),
				})
			})
		}
	})

	t.Run("fuzz", func(t *testing.T) {
		t.Parallel()

		for i := 0; i < 10000; i++ {
			h := Header{
				Fin:    randBool(),
				RSV1:   randBool(),
				RSV2:   randBool(),
				RSV3:   randBool(),
				Opcode: Opcode(rand.Intn(1 << 4)),

				Masked:        randBool(),
				PayloadLength: rand.Int63(),
			}

			if h.Masked {
				h.MaskKey = rand.Uint32()
			}

			testHeader(t, h)
		}
	})
}

func testHeader(t *testing.T, h Header) {
	b := h.Bytes(nil)
	r := bytes.NewReader(b)
	h2, err := ReadHeader(r, nil)
	if err != nil {
		t.Logf("Header: %#v", h)
		t.Logf("bytes: %b", b)
		t.Fatalf("failed to read Header: %v", err)
	}

	if !cmp.Equal(h, h2, cmp.AllowUnexported(Header{})) {
		t.Logf("Header: %#v", h)
		t.Logf("bytes: %b", b)
		t.Fatalf("parsed and read Header differ: %v", cmp.Diff(h, h2, cmp.AllowUnexported(Header{})))
	}
}
