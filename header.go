package ws

import (
	"encoding/binary"
	"fmt"
	"io"
)

type opCode int

const (
	opContinuation opCode = iota
	opText
	opBinary
	// 3 - 7 are reserved for further non-control frames.
	opClose = 8
	opPing
	opPong
	// 11-16 are reserved for further control frames.
)

// DataOpcode is a WebSocket data opcode.
// This is how the WebSocket RFC capitalizes Opcode.
// TODO we don't just always binary because text utf-8 and javascript convert to utf-16 optimizations.
type DataOpcode int

const (
	OpText   = DataOpcode(opText)
	OpBinary = DataOpcode(opBinary)
)

type header struct {
	fin    bool
	opcode opCode
	// Length is an integer because the RFC mandates the MSB bit cannot be set.
	// So we cannot send or receive a frame with negative length.
	length int64

	masked bool
	mask   [4]byte
}

const maxHeaderSize = 2 + 8 + 4

// TODO Benchmark ptr
func (f *header) writeTo(w io.Writer) (int64, error) {
	var b [maxHeaderSize]byte

	if f.fin {
		b[0] |= 0x80
	}
	// Next 3 bits in the first byte are for extensions so we never set them.

	if f.opcode > 0x0F {
		return nil, fmt.Errorf("opcode not allowed to be greater than 0x0F: %")
		panicf("opcode is not allowed to be greater than 0x0F: %#v", f.opcode)
	}

	b[0] |= byte(f.opcode)

	length := 2

	switch {
	case f.length < 0:
		panicf("length is not allowed to be less than 0: %#v", f.length)
	case f.length < 126:
		b[1] |= byte(f.length)
	case f.length < 65536:
		b[1] = 126
		length += 2
		binary.BigEndian.PutUint16(b[length:], uint16(f.length))
	default:
		b[1] = 127
		length += 8
		binary.BigEndian.PutUint16(b[length:], uint16(f.length))
	}

	if f.masked {
		b[1] |= 0x80
		length += copy(b[length:], f.mask[:])
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
