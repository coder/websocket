package websocket

import (
	"bytes"
	"math/rand"
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

	for i := 0; i < 1000; i++ {
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

		t.Logf("header: %#v", h)

		b := marshalHeader(h)
		t.Logf("bytes: %b", b)

		r := bytes.NewReader(b)
		h2, err := readHeader(r)
		if err != nil {
			t.Fatalf("failed to read header: %v", err)
		}

		if !cmp.Equal(h, h2, cmp.AllowUnexported(header{})) {
			t.Fatalf("parsed and read header differ: %v", cmp.Diff(h, h2, cmp.AllowUnexported(header{})))
		}
	}
}
