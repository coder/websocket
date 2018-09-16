package ws

import (
	"bytes"
	"testing"
)

func TestHeader(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		h    header
		want []byte
	}{
		{
			name: "RFC1",
			h: header{
				fin:    true,
				opcode: OpText,
				length: int64(len("Hello")),
			},
			want: []byte{
				0x81,
				0x05,
			},
		},
		{
			name: "RFC2",
			h: header{
				fin:    true,
				opcode: OpText,
				length: int64(len("Hello")),
				masked: true,
				mask: [4]byte{
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
			b := tc.h.writeTo()
			if !bytes.Equal(tc.want, b) {
				t.Errorf("want %#v; got %#v", tc.want, b)
			}
		})
	}
}
