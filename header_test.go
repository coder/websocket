package ws_test

import (
	"bytes"
	"testing"

	"nhooyr.io/ws"
)

func TestHeader(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		h    ws.Header
		want []byte
	}{
		{
			name: "RFC1",
			h: ws.Header{
				FIN:    true,
				Opcode: ws.OpText,
				Length: int64(len("Hello")),
			},
			want: []byte{
				0x81,
				0x05,
			},
		},
		{
			name: "RFC2",
			h: ws.Header{
				FIN:    true,
				Opcode: ws.OpText,
				Length: int64(len("Hello")),
				Masked: true,
				Mask: [4]byte{
					0x37,
					0xfa,
					0x21,
					0x3d,
				},
			},
			want: []byte{
				0x81,
				0x85,
				0x37,
				0xfa,
				0x21,
				0x3d,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.h.Bytes()
			if !bytes.Equal(tc.want, b) {
				t.Errorf("want %#v; got %#v", tc.want, b)
			}
		})
	}
}
