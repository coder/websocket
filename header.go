package ws

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Opcode is a WebSocket opcode.
// This is how the WebSocket RFC capitalizes it.
type Opcode int

const (
	opContinuation Opcode = iota
	OpText
	OpBinary
	// 3 - 7 are reserved for further non-control frames.
	OpClose = 8
	OpPing
	OpPong
	// 11-16 are reserved for further control frames.
)

type header struct {
	FIN bool

	RSV1 bool
	RSV2 bool
	RSV3 bool

	Opcode Opcode

	// Length is an integer because the RFC mandates the MSB bit cannot be set.
	// So we cannot send or receive a frame with negative length.
	Length int64

	Masked bool
	Mask   [4]byte
}

const maxHeaderSize = 2 + 8 + 4

// TODO turn into WriteTo to make use of buffering better.
// TODO Benchmark ptr
func (f *header) Bytes() []byte {
	var b [maxHeaderSize]byte

	if f.FIN {
		b[0] |= 0x80
	}
	if f.RSV1 {
		b[0] |= 0x40
	}
	if f.RSV2 {
		b[0] |= 0x20
	}
	if f.RSV3 {
		b[0] |= 0x10
	}

	if f.Opcode > 0x0F {
		panicf("opcode is not allowed to be greater than 0x0F: %#v", f.Opcode)
	}

	b[0] |= byte(f.Opcode)

	length := 2

	switch {
	case f.Length < 0:
		panicf("length is not allowed to be less than 0: %#v", f.Length)
	case f.Length < 126:
		b[1] |= byte(f.Length)
	case f.Length < 65536:
		b[1] = 126
		length += 2
		binary.BigEndian.PutUint16(b[length:], uint16(f.Length))
	default:
		b[1] = 127
		length += 8
		binary.BigEndian.PutUint16(b[length:], uint16(f.Length))
	}

	if f.Masked {
		b[1] |= 0x80
		length += copy(b[length:], f.Mask[:])
	}

	return b[:length]
}

func readHeader(w io.Writer) (header, error) {
	panic("TODO")
}

func panicf(f string, v ...interface{}) {
	msg := fmt.Sprintf(f, v...)
	panic(msg)
}
