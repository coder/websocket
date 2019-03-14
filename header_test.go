package ws

import (
	"bytes"
	"reflect"
	"testing"
)

func TestHeader(t *testing.T) {
	t.Parallel()

	// TODO can have more tests for error cases that set raw bytes wanted/as input
	testCases := []struct {
		name string
		h    header
	}{
		{
			name: "RFC1",
			h: header{
				fin:           true,
				opCode:        opText,
				payloadLength: int64(len("Hello")),
			},
		},
		{
			name: "RFC2",
			h: header{
				fin:           true,
				opCode:        opText,
				payloadLength: int64(len("Hello")),
				masked:        true,
				maskKey: [4]byte{
					0x37,
					0xfa,
					0x21,
					0x3d,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b := &bytes.Buffer{}
			_, err := tc.h.writeTo(b)
			if err != nil {
				t.Fatalf("failed to write header: %v", err)
			}

			h, err := readHeader(b)
			if err != nil {
				t.Fatalf("failed to read header: %v", err)
			}

			if reflect.DeepEqual(tc.h, h) {
				t.Errorf("want %#v; got %#v", tc.h, h)
			}
		})
	}
}
