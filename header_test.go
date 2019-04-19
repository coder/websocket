package websocket

import (
	"bytes"
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func randBool() bool {
	return rand.Intn(1) == 0
}

func TestHeader(t *testing.T) {
	t.Parallel()

	t.Run("readNegativeLength", func(t *testing.T) {
		t.Parallel()

		b := marshalHeader(header{
			payloadLength: 1<<16 + 1,
		})

		// Make length negative
		b[2] |= 1 << 7

		r := bytes.NewReader(b)
		_, err := readHeader(r)
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

				testHeader(t, header{
					payloadLength: int64(n),
				})
			})
		}
	})

	t.Run("fuzz", func(t *testing.T) {
		t.Parallel()

		for i := 0; i < 10000; i++ {
			h := header{
				fin:    randBool(),
				rsv1:   randBool(),
				rsv2:   randBool(),
				rsv3:   randBool(),
				opcode: opcode(rand.Intn(1 << 4)),

				masked:        randBool(),
				payloadLength: rand.Int63(),
			}

			if h.masked {
				rand.Read(h.maskKey[:])
			}

			testHeader(t, h)
		}
	})
}

func testHeader(t *testing.T, h header) {
	b := marshalHeader(h)
	r := bytes.NewReader(b)
	h2, err := readHeader(r)
	if err != nil {
		t.Logf("header: %#v", h)
		t.Logf("bytes: %b", b)
		t.Fatalf("failed to read header: %v", err)
	}

	if !cmp.Equal(h, h2, cmp.AllowUnexported(header{})) {
		t.Logf("header: %#v", h)
		t.Logf("bytes: %b", b)
		t.Fatalf("parsed and read header differ: %v", cmp.Diff(h, h2, cmp.AllowUnexported(header{})))
	}
}
