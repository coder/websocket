// +build !js

package websocket

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"math/bits"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"
	_ "unsafe"

	"github.com/gobwas/ws"
	"github.com/google/go-cmp/cmp"
	_ "github.com/gorilla/websocket"

	"nhooyr.io/websocket/internal/assert"
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
				_, err := readHeader(nil, b)
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

		writeHeader(nil, header{
			payloadLength: -1,
		})
	})

	t.Run("readNegativeLength", func(t *testing.T) {
		t.Parallel()

		b := writeHeader(nil, header{
			payloadLength: 1<<16 + 1,
		})

		// Make length negative
		b[2] |= 1 << 7

		r := bytes.NewReader(b)
		_, err := readHeader(nil, r)
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
				h.maskKey = rand.Uint32()
			}

			testHeader(t, h)
		}
	})
}

func testHeader(t *testing.T, h header) {
	b := writeHeader(nil, h)
	r := bytes.NewReader(b)
	h2, err := readHeader(nil, r)
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

func TestCloseError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		ce      CloseError
		success bool
	}{
		{
			name: "normal",
			ce: CloseError{
				Code:   StatusNormalClosure,
				Reason: strings.Repeat("x", maxControlFramePayload-2),
			},
			success: true,
		},
		{
			name: "bigReason",
			ce: CloseError{
				Code:   StatusNormalClosure,
				Reason: strings.Repeat("x", maxControlFramePayload-1),
			},
			success: false,
		},
		{
			name: "bigCode",
			ce: CloseError{
				Code:   math.MaxUint16,
				Reason: strings.Repeat("x", maxControlFramePayload-2),
			},
			success: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := tc.ce.bytes()
			if (err == nil) != tc.success {
				t.Fatalf("unexpected error value: %+v", err)
			}
		})
	}
}

func Test_parseClosePayload(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		p       []byte
		success bool
		ce      CloseError
	}{
		{
			name:    "normal",
			p:       append([]byte{0x3, 0xE8}, []byte("hello")...),
			success: true,
			ce: CloseError{
				Code:   StatusNormalClosure,
				Reason: "hello",
			},
		},
		{
			name:    "nothing",
			success: true,
			ce: CloseError{
				Code: StatusNoStatusRcvd,
			},
		},
		{
			name:    "oneByte",
			p:       []byte{0},
			success: false,
		},
		{
			name:    "badStatusCode",
			p:       []byte{0x17, 0x70},
			success: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ce, err := parseClosePayload(tc.p)
			if (err == nil) != tc.success {
				t.Fatalf("unexpected expected error value: %+v", err)
			}

			if tc.success && tc.ce != ce {
				t.Fatalf("unexpected close error: %v", cmp.Diff(tc.ce, ce))
			}
		})
	}
}

func Test_validWireCloseCode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		code  StatusCode
		valid bool
	}{
		{
			name:  "normal",
			code:  StatusNormalClosure,
			valid: true,
		},
		{
			name:  "noStatus",
			code:  StatusNoStatusRcvd,
			valid: false,
		},
		{
			name:  "3000",
			code:  3000,
			valid: true,
		},
		{
			name:  "4999",
			code:  4999,
			valid: true,
		},
		{
			name:  "unknown",
			code:  5000,
			valid: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if valid := validWireCloseCode(tc.code); tc.valid != valid {
				t.Fatalf("expected %v for %v but got %v", tc.valid, tc.code, valid)
			}
		})
	}
}

func Test_mask(t *testing.T) {
	t.Parallel()

	key := []byte{0xa, 0xb, 0xc, 0xff}
	key32 := binary.LittleEndian.Uint32(key)
	p := []byte{0xa, 0xb, 0xc, 0xf2, 0xc}
	gotKey32 := mask(key32, p)

	if exp := []byte{0, 0, 0, 0x0d, 0x6}; !cmp.Equal(exp, p) {
		t.Fatalf("unexpected mask: %v", cmp.Diff(exp, p))
	}

	if exp := bits.RotateLeft32(key32, -8); !cmp.Equal(exp, gotKey32) {
		t.Fatalf("unexpected mask key: %v", cmp.Diff(exp, gotKey32))
	}
}

func basicMask(maskKey [4]byte, pos int, b []byte) int {
	for i := range b {
		b[i] ^= maskKey[pos&3]
		pos++
	}
	return pos & 3
}

//go:linkname gorillaMaskBytes github.com/gorilla/websocket.maskBytes
func gorillaMaskBytes(key [4]byte, pos int, b []byte) int

func Benchmark_mask(b *testing.B) {
	sizes := []int{
		2,
		3,
		4,
		8,
		16,
		32,
		128,
		512,
		4096,
		16384,
	}

	fns := []struct {
		name string
		fn   func(b *testing.B, key [4]byte, p []byte)
	}{
		{
			name: "basic",
			fn: func(b *testing.B, key [4]byte, p []byte) {
				for i := 0; i < b.N; i++ {
					basicMask(key, 0, p)
				}
			},
		},

		{
			name: "nhooyr",
			fn: func(b *testing.B, key [4]byte, p []byte) {
				key32 := binary.LittleEndian.Uint32(key[:])
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					mask(key32, p)
				}
			},
		},
		{
			name: "gorilla",
			fn: func(b *testing.B, key [4]byte, p []byte) {
				for i := 0; i < b.N; i++ {
					gorillaMaskBytes(key, 0, p)
				}
			},
		},
		{
			name: "gobwas",
			fn: func(b *testing.B, key [4]byte, p []byte) {
				for i := 0; i < b.N; i++ {
					ws.Cipher(p, key, 0)
				}
			},
		},
	}

	var key [4]byte
	_, err := rand.Read(key[:])
	if err != nil {
		b.Fatalf("failed to populate mask key: %v", err)
	}

	for _, size := range sizes {
		p := make([]byte, size)

		b.Run(strconv.Itoa(size), func(b *testing.B) {
			for _, fn := range fns {
				b.Run(fn.name, func(b *testing.B) {
					b.SetBytes(int64(size))

					fn.fn(b, key, p)
				})
			}
		})
	}
}

func TestCloseStatus(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		in   error
		exp  StatusCode
	}{
		{
			name: "nil",
			in:   nil,
			exp:  -1,
		},
		{
			name: "io.EOF",
			in:   io.EOF,
			exp:  -1,
		},
		{
			name: "StatusInternalError",
			in: CloseError{
				Code: StatusInternalError,
			},
			exp: StatusInternalError,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := assert.Equalf(tc.exp, CloseStatus(tc.in), "unexpected close status")
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
