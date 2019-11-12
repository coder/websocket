package wsframe

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// Opcode represents a WebSocket Opcode.
type Opcode int

// Opcode constants.
const (
	OpContinuation Opcode = iota
	OpText
	OpBinary
	// 3 - 7 are reserved for further non-control frames.
	_
	_
	_
	_
	_
	OpClose
	OpPing
	OpPong
	// 11-16 are reserved for further control frames.
)

func (o Opcode) Control() bool {
	switch o {
	case OpClose, OpPing, OpPong:
		return true
	}
	return false
}

func (o Opcode) Data() bool {
	switch o {
	case OpText, OpBinary:
		return true
	}
	return false
}

// First byte contains fin, rsv1, rsv2, rsv3.
// Second byte contains mask flag and payload len.
// Next 8 bytes are the maximum extended payload length.
// Last 4 bytes are the mask key.
// https://tools.ietf.org/html/rfc6455#section-5.2
const maxHeaderSize = 1 + 1 + 8 + 4

// Header represents a WebSocket frame Header.
// See https://tools.ietf.org/html/rfc6455#section-5.2
type Header struct {
	Fin    bool
	RSV1   bool
	RSV2   bool
	RSV3   bool
	Opcode Opcode

	PayloadLength int64

	Masked  bool
	MaskKey uint32
}

// bytes returns the bytes of the Header.
// See https://tools.ietf.org/html/rfc6455#section-5.2
func (h Header) Bytes(b []byte) []byte {
	if b == nil {
		b = make([]byte, maxHeaderSize)
	}

	b = b[:2]
	b[0] = 0

	if h.Fin {
		b[0] |= 1 << 7
	}
	if h.RSV1 {
		b[0] |= 1 << 6
	}
	if h.RSV2 {
		b[0] |= 1 << 5
	}
	if h.RSV3 {
		b[0] |= 1 << 4
	}

	b[0] |= byte(h.Opcode)

	switch {
	case h.PayloadLength < 0:
		panic(fmt.Sprintf("websocket: invalid Header: negative length: %v", h.PayloadLength))
	case h.PayloadLength <= 125:
		b[1] = byte(h.PayloadLength)
	case h.PayloadLength <= math.MaxUint16:
		b[1] = 126
		b = b[:len(b)+2]
		binary.BigEndian.PutUint16(b[len(b)-2:], uint16(h.PayloadLength))
	default:
		b[1] = 127
		b = b[:len(b)+8]
		binary.BigEndian.PutUint64(b[len(b)-8:], uint64(h.PayloadLength))
	}

	if h.Masked {
		b[1] |= 1 << 7
		b = b[:len(b)+4]
		binary.LittleEndian.PutUint32(b[len(b)-4:], h.MaskKey)
	}

	return b
}

func MakeReadHeaderBuf() []byte {
	return make([]byte, maxHeaderSize-2)
}

// ReadHeader reads a Header from the reader.
// See https://tools.ietf.org/html/rfc6455#section-5.2
func ReadHeader(r io.Reader, b []byte) (Header, error) {
	// We read the first two bytes first so that we know
	// exactly how long the Header is.
	b = b[:2]
	_, err := io.ReadFull(r, b)
	if err != nil {
		return Header{}, err
	}

	var h Header
	h.Fin = b[0]&(1<<7) != 0
	h.RSV1 = b[0]&(1<<6) != 0
	h.RSV2 = b[0]&(1<<5) != 0
	h.RSV3 = b[0]&(1<<4) != 0

	h.Opcode = Opcode(b[0] & 0xf)

	var extra int

	h.Masked = b[1]&(1<<7) != 0
	if h.Masked {
		extra += 4
	}

	payloadLength := b[1] &^ (1 << 7)
	switch {
	case payloadLength < 126:
		h.PayloadLength = int64(payloadLength)
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
		return Header{}, err
	}

	switch {
	case payloadLength == 126:
		h.PayloadLength = int64(binary.BigEndian.Uint16(b))
		b = b[2:]
	case payloadLength == 127:
		h.PayloadLength = int64(binary.BigEndian.Uint64(b))
		if h.PayloadLength < 0 {
			return Header{}, fmt.Errorf("Header with negative payload length: %v", h.PayloadLength)
		}
		b = b[8:]
	}

	if h.Masked {
		h.MaskKey = binary.LittleEndian.Uint32(b)
	}

	return h, nil
}

const MaxControlFramePayload = 125

func ParseClosePayload(p []byte) (uint16, string, error) {
	if len(p) < 2 {
		return 0, "", fmt.Errorf("close payload %q too small, cannot even contain the 2 byte status code", p)
	}

	return binary.BigEndian.Uint16(p), string(p[2:]), nil
}
