package ws

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// DataOpcode is a WebSocket data opcode.
// This is how the WebSocket RFC capitalizes Opcode.
// We allow users to set this, even though all data is technically binary because text frames appear
// in javascript as UTF-16 strings.
//go:generate stringer -type=opcode
type opcode int

const (
	opContinuation opcode = iota
	opText
	opBinary
	// 3 - 7 are reserved for further non-control frames.g
	opClose opcode = 8 + iota
	opPing
	opPong
	// 11-16 are reserved for further control frames.
)

// This is how Opcode is in the RFC.
type DataOpcode opcode

const (
	OpText   = DataOpcode(opText)
	OpBinary = DataOpcode(opBinary)
)

func (op DataOpcode) String() string {
	return opcode(op).String()
}

type header struct {
	fin    bool
	opCode opcode
	// payloadLength is an integer because the RFC mandates the MSB bit cannot be set.
	// So we cannot send or receive a frame with negative length.
	payloadLength int64

	masked  bool
	maskKey [4]byte
}

// First byte consists of FIN, RSV1, RSV2, RSV3 and the Opcode.
// Second byte is the mask flag and the payload length.
// Next 8 bytes are the extended payload length.
// Next 4 bytes are the mask key.
const maxHeaderSize = 1 + 1 + 8 + 4

// See https://tools.ietf.org/html/rfc6455#section-5.2
func (f header) bytes() ([]byte, error) {
	b := make([]byte, maxHeaderSize)

	if f.fin {
		b[0] |= 0x80
	}
	// Next 3 bits in the first byte are for extensions so we never set them.

	// Opcode can only be max 4 bits.
	if f.opCode > 1<<4-1 {
		return nil, fmt.Errorf("opcode not allowed to be greater than 0x0f: %#x", f.opCode)
	}

	b[0] |= byte(f.opCode)

	// Minimum length of the frame as we have at least one more byte
	// for the mask bit and the payload length.
	length := 1 + 1

	switch {
	case f.payloadLength < 0:
		return nil, fmt.Errorf("length is not permitted to be negative: %#x", f.payloadLength)
	case f.payloadLength < 126:
		b[1] |= byte(f.payloadLength)
	case f.payloadLength <= math.MaxUint16:
		b[1] = 126
		binary.BigEndian.PutUint16(b[length:], uint16(f.payloadLength))
		length += 2
	default:
		b[1] = 127
		binary.BigEndian.PutUint64(b[length:], uint64(f.payloadLength))
		length += 8
	}

	if f.masked {
		b[1] |= 1 << 7
		length += copy(b[length:], f.maskKey[:])
	}

	return b[:length], nil
}

// See https://tools.ietf.org/html/rfc6455#section-5.2
func readHeader(r io.Reader) (header, error) {
	var b [maxHeaderSize - 2]byte

	_, err := io.ReadFull(r, b[:2])
	if err != nil {
		return header{}, fmt.Errorf("failed to read first 2 bytes of header: %v", err)
	}

	var h header

	h.fin = b[0]&(1<<3) != 0
	h.opCode = opcode(b[0] & 0x0f)
	h.masked = b[1]&(1<<7) != 0

	extra := 0

	if h.masked {
		extra += 4
	}

	payloadLength := b[1] &^ (1 << 7)
	switch {
	case payloadLength == 127:
		extra += 8
	case payloadLength == 126:
		extra += 2
	}

	_, err = io.ReadFull(r, b[:extra])
	if err != nil {
		return header{}, fmt.Errorf("failed to read remaining header bytes: %v", err)
	}

	switch payloadLength {
	case 127:
		h.payloadLength = int64(binary.BigEndian.Uint64(b[:]))
		if h.payloadLength < 0 {
			// TODO is this necessary?
			return header{}, fmt.Errorf("received disallowed negative payload length: %#x", h.payloadLength)
		}
	case 126:
		h.payloadLength = int64(binary.BigEndian.Uint16(b[:]))
	default:
		h.payloadLength = int64(payloadLength)
	}

	if h.masked {
		// Skip the extended payload length.
		extra -= 4
		copy(h.maskKey[:], b[extra:])
	}

	return h, nil
}
