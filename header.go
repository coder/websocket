package websocket

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"golang.org/x/xerrors"
)

// First byte contains fin, rsv1, rsv2, rsv3.
// Second byte contains mask flag and payload len.
// Next 8 bytes are the maximum extended payload length.
// Last 4 bytes are the mask key.
// https://tools.ietf.org/html/rfc6455#section-5.2
const maxHeaderSize = 1 + 1 + 8 + 4

// header represents a WebSocket frame header.
// See https://tools.ietf.org/html/rfc6455#section-5.2
type header struct {
	fin    bool
	rsv1   bool
	rsv2   bool
	rsv3   bool
	opcode opcode

	payloadLength int64

	masked  bool
	maskKey [4]byte
}

// bytes returns the bytes of the header.
// See https://tools.ietf.org/html/rfc6455#section-5.2
func marshalHeader(h header) []byte {
	b := make([]byte, 2, maxHeaderSize)

	if h.fin {
		b[0] |= 1 << 7
	}
	if h.rsv1 {
		b[0] |= 1 << 6
	}
	if h.rsv2 {
		b[0] |= 1 << 5
	}
	if h.rsv3 {
		b[0] |= 1 << 4
	}

	b[0] |= byte(h.opcode)

	switch {
	case h.payloadLength < 0:
		panic(fmt.Sprintf("websocket: invalid header: negative length: %v", h.payloadLength))
	case h.payloadLength <= 125:
		b[1] = byte(h.payloadLength)
	case h.payloadLength <= math.MaxUint16:
		b[1] = 126
		b = b[:len(b)+2]
		binary.BigEndian.PutUint16(b[len(b)-2:], uint16(h.payloadLength))
	default:
		b[1] = 127
		b = b[:len(b)+8]
		binary.BigEndian.PutUint64(b[len(b)-8:], uint64(h.payloadLength))
	}

	if h.masked {
		b[1] |= 1 << 7
		b = b[:len(b)+4]
		copy(b[len(b)-4:], h.maskKey[:])
	}

	return b
}

// readHeader reads a header from the reader.
// See https://tools.ietf.org/html/rfc6455#section-5.2
func readHeader(r io.Reader) (header, error) {
	// We read the first two bytes directly so that we know
	// exactly how long the header is.
	b := make([]byte, 2, maxHeaderSize-2)
	_, err := io.ReadFull(r, b)
	if err != nil {
		return header{}, err
	}

	var h header
	h.fin = b[0]&(1<<7) != 0
	h.rsv1 = b[0]&(1<<6) != 0
	h.rsv2 = b[0]&(1<<5) != 0
	h.rsv3 = b[0]&(1<<4) != 0

	h.opcode = opcode(b[0] & 0xf)

	var extra int

	h.masked = b[1]&(1<<7) != 0
	if h.masked {
		extra += 4
	}

	payloadLength := b[1] &^ (1 << 7)
	switch {
	case payloadLength < 126:
		h.payloadLength = int64(payloadLength)
	case payloadLength == 126:
		extra += 2
	case payloadLength == 127:
		extra += 8
	}

	if extra == 0 {
		return h, nil
	}

	b = b[:extra]
	_, err = io.ReadFull(r, b)
	if err != nil {
		return header{}, err
	}

	switch {
	case payloadLength == 126:
		h.payloadLength = int64(binary.BigEndian.Uint16(b))
		b = b[2:]
	case payloadLength == 127:
		h.payloadLength = int64(binary.BigEndian.Uint64(b))
		if h.payloadLength < 0 {
			return header{}, xerrors.Errorf("websocket: header has negative payload length: %v", h.payloadLength)
		}
		b = b[8:]
	}

	if h.masked {
		copy(h.maskKey[:], b)
	}

	return h, nil
}
