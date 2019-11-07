package websocket

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/bits"
)

//go:generate stringer -type=opcode,MessageType,StatusCode -output=frame_stringer.go

// opcode represents a WebSocket Opcode.
type opcode int

// opcode constants.
const (
	opContinuation opcode = iota
	opText
	opBinary
	// 3 - 7 are reserved for further non-control frames.
	_
	_
	_
	_
	_
	opClose
	opPing
	opPong
	// 11-16 are reserved for further control frames.
)

func (o opcode) controlOp() bool {
	switch o {
	case opClose, opPing, opPong:
		return true
	}
	return false
}

// MessageType represents the type of a WebSocket message.
// See https://tools.ietf.org/html/rfc6455#section-5.6
type MessageType int

// MessageType constants.
const (
	// MessageText is for UTF-8 encoded text messages like JSON.
	MessageText MessageType = iota + 1
	// MessageBinary is for binary messages like Protobufs.
	MessageBinary
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
	maskKey uint32
}

func makeWriteHeaderBuf() []byte {
	return make([]byte, maxHeaderSize)
}

// bytes returns the bytes of the header.
// See https://tools.ietf.org/html/rfc6455#section-5.2
func writeHeader(b []byte, h header) []byte {
	if b == nil {
		b = makeWriteHeaderBuf()
	}

	b = b[:2]
	b[0] = 0

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
		binary.LittleEndian.PutUint32(b[len(b)-4:], h.maskKey)
	}

	return b
}

func makeReadHeaderBuf() []byte {
	return make([]byte, maxHeaderSize-2)
}

// readHeader reads a header from the reader.
// See https://tools.ietf.org/html/rfc6455#section-5.2
func readHeader(b []byte, r io.Reader) (header, error) {
	if b == nil {
		b = makeReadHeaderBuf()
	}

	// We read the first two bytes first so that we know
	// exactly how long the header is.
	b = b[:2]
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
			return header{}, fmt.Errorf("header with negative payload length: %v", h.payloadLength)
		}
		b = b[8:]
	}

	if h.masked {
		h.maskKey = binary.LittleEndian.Uint32(b)
	}

	return h, nil
}

// StatusCode represents a WebSocket status code.
// https://tools.ietf.org/html/rfc6455#section-7.4
type StatusCode int

// These codes were retrieved from:
// https://www.iana.org/assignments/websocket/websocket.xhtml#close-code-number
//
// The defined constants only represent the status codes registered with IANA.
// The 4000-4999 range of status codes is reserved for arbitrary use by applications.
const (
	StatusNormalClosure   StatusCode = 1000
	StatusGoingAway       StatusCode = 1001
	StatusProtocolError   StatusCode = 1002
	StatusUnsupportedData StatusCode = 1003

	// 1004 is reserved and so not exported.
	statusReserved StatusCode = 1004

	// StatusNoStatusRcvd cannot be sent in a close message.
	// It is reserved for when a close message is received without
	// an explicit status.
	StatusNoStatusRcvd StatusCode = 1005

	// StatusAbnormalClosure is only exported for use with Wasm.
	// In non Wasm Go, the returned error will indicate whether the connection was closed or not or what happened.
	StatusAbnormalClosure StatusCode = 1006

	StatusInvalidFramePayloadData StatusCode = 1007
	StatusPolicyViolation         StatusCode = 1008
	StatusMessageTooBig           StatusCode = 1009
	StatusMandatoryExtension      StatusCode = 1010
	StatusInternalError           StatusCode = 1011
	StatusServiceRestart          StatusCode = 1012
	StatusTryAgainLater           StatusCode = 1013
	StatusBadGateway              StatusCode = 1014

	// StatusTLSHandshake is only exported for use with Wasm.
	// In non Wasm Go, the returned error will indicate whether there was a TLS handshake failure.
	StatusTLSHandshake StatusCode = 1015
)

// CloseError represents a WebSocket close frame.
// It is returned by Conn's methods when a WebSocket close frame is received from
// the peer.
// You will need to use the https://golang.org/pkg/errors/#As function, new in Go 1.13,
// to check for this error. See the CloseError example.
type CloseError struct {
	Code   StatusCode
	Reason string
}

func (ce CloseError) Error() string {
	return fmt.Sprintf("status = %v and reason = %q", ce.Code, ce.Reason)
}

// CloseStatus is a convenience wrapper around errors.As to grab
// the status code from a *CloseError. If the passed error is nil
// or not a *CloseError, the returned StatusCode will be -1.
func CloseStatus(err error) StatusCode {
	var ce CloseError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return -1
}

func parseClosePayload(p []byte) (CloseError, error) {
	if len(p) == 0 {
		return CloseError{
			Code: StatusNoStatusRcvd,
		}, nil
	}

	if len(p) < 2 {
		return CloseError{}, fmt.Errorf("close payload %q too small, cannot even contain the 2 byte status code", p)
	}

	ce := CloseError{
		Code:   StatusCode(binary.BigEndian.Uint16(p)),
		Reason: string(p[2:]),
	}

	if !validWireCloseCode(ce.Code) {
		return CloseError{}, fmt.Errorf("invalid status code %v", ce.Code)
	}

	return ce, nil
}

// See http://www.iana.org/assignments/websocket/websocket.xhtml#close-code-number
// and https://tools.ietf.org/html/rfc6455#section-7.4.1
func validWireCloseCode(code StatusCode) bool {
	switch code {
	case statusReserved, StatusNoStatusRcvd, StatusAbnormalClosure, StatusTLSHandshake:
		return false
	}

	if code >= StatusNormalClosure && code <= StatusBadGateway {
		return true
	}
	if code >= 3000 && code <= 4999 {
		return true
	}

	return false
}

const maxControlFramePayload = 125

func (ce CloseError) bytes() ([]byte, error) {
	if len(ce.Reason) > maxControlFramePayload-2 {
		return nil, fmt.Errorf("reason string max is %v but got %q with length %v", maxControlFramePayload-2, ce.Reason, len(ce.Reason))
	}
	if !validWireCloseCode(ce.Code) {
		return nil, fmt.Errorf("status code %v cannot be set", ce.Code)
	}

	buf := make([]byte, 2+len(ce.Reason))
	binary.BigEndian.PutUint16(buf, uint16(ce.Code))
	copy(buf[2:], ce.Reason)
	return buf, nil
}

// fastMask applies the WebSocket masking algorithm to p
// with the given key.
// See https://tools.ietf.org/html/rfc6455#section-5.3
//
// The returned value is the correctly rotated key to
// to continue to mask/unmask the message.
//
// It is optimized for LittleEndian and expects the key
// to be in little endian.
//
// See https://github.com/golang/go/issues/31586
func mask(key uint32, b []byte) uint32 {
	if len(b) >= 8 {
		key64 := uint64(key)<<32 | uint64(key)

		// At some point in the future we can clean these unrolled loops up.
		// See https://github.com/golang/go/issues/31586#issuecomment-487436401

		// Then we xor until b is less than 128 bytes.
		for len(b) >= 128 {
			v := binary.LittleEndian.Uint64(b)
			binary.LittleEndian.PutUint64(b, v^key64)
			v = binary.LittleEndian.Uint64(b[8:16])
			binary.LittleEndian.PutUint64(b[8:16], v^key64)
			v = binary.LittleEndian.Uint64(b[16:24])
			binary.LittleEndian.PutUint64(b[16:24], v^key64)
			v = binary.LittleEndian.Uint64(b[24:32])
			binary.LittleEndian.PutUint64(b[24:32], v^key64)
			v = binary.LittleEndian.Uint64(b[32:40])
			binary.LittleEndian.PutUint64(b[32:40], v^key64)
			v = binary.LittleEndian.Uint64(b[40:48])
			binary.LittleEndian.PutUint64(b[40:48], v^key64)
			v = binary.LittleEndian.Uint64(b[48:56])
			binary.LittleEndian.PutUint64(b[48:56], v^key64)
			v = binary.LittleEndian.Uint64(b[56:64])
			binary.LittleEndian.PutUint64(b[56:64], v^key64)
			v = binary.LittleEndian.Uint64(b[64:72])
			binary.LittleEndian.PutUint64(b[64:72], v^key64)
			v = binary.LittleEndian.Uint64(b[72:80])
			binary.LittleEndian.PutUint64(b[72:80], v^key64)
			v = binary.LittleEndian.Uint64(b[80:88])
			binary.LittleEndian.PutUint64(b[80:88], v^key64)
			v = binary.LittleEndian.Uint64(b[88:96])
			binary.LittleEndian.PutUint64(b[88:96], v^key64)
			v = binary.LittleEndian.Uint64(b[96:104])
			binary.LittleEndian.PutUint64(b[96:104], v^key64)
			v = binary.LittleEndian.Uint64(b[104:112])
			binary.LittleEndian.PutUint64(b[104:112], v^key64)
			v = binary.LittleEndian.Uint64(b[112:120])
			binary.LittleEndian.PutUint64(b[112:120], v^key64)
			v = binary.LittleEndian.Uint64(b[120:128])
			binary.LittleEndian.PutUint64(b[120:128], v^key64)
			b = b[128:]
		}

		// Then we xor until b is less than 64 bytes.
		for len(b) >= 64 {
			v := binary.LittleEndian.Uint64(b)
			binary.LittleEndian.PutUint64(b, v^key64)
			v = binary.LittleEndian.Uint64(b[8:16])
			binary.LittleEndian.PutUint64(b[8:16], v^key64)
			v = binary.LittleEndian.Uint64(b[16:24])
			binary.LittleEndian.PutUint64(b[16:24], v^key64)
			v = binary.LittleEndian.Uint64(b[24:32])
			binary.LittleEndian.PutUint64(b[24:32], v^key64)
			v = binary.LittleEndian.Uint64(b[32:40])
			binary.LittleEndian.PutUint64(b[32:40], v^key64)
			v = binary.LittleEndian.Uint64(b[40:48])
			binary.LittleEndian.PutUint64(b[40:48], v^key64)
			v = binary.LittleEndian.Uint64(b[48:56])
			binary.LittleEndian.PutUint64(b[48:56], v^key64)
			v = binary.LittleEndian.Uint64(b[56:64])
			binary.LittleEndian.PutUint64(b[56:64], v^key64)
			b = b[64:]
		}

		// Then we xor until b is less than 32 bytes.
		for len(b) >= 32 {
			v := binary.LittleEndian.Uint64(b)
			binary.LittleEndian.PutUint64(b, v^key64)
			v = binary.LittleEndian.Uint64(b[8:16])
			binary.LittleEndian.PutUint64(b[8:16], v^key64)
			v = binary.LittleEndian.Uint64(b[16:24])
			binary.LittleEndian.PutUint64(b[16:24], v^key64)
			v = binary.LittleEndian.Uint64(b[24:32])
			binary.LittleEndian.PutUint64(b[24:32], v^key64)
			b = b[32:]
		}

		// Then we xor until b is less than 16 bytes.
		for len(b) >= 16 {
			v := binary.LittleEndian.Uint64(b)
			binary.LittleEndian.PutUint64(b, v^key64)
			v = binary.LittleEndian.Uint64(b[8:16])
			binary.LittleEndian.PutUint64(b[8:16], v^key64)
			b = b[16:]
		}

		// Then we xor until b is less than 8 bytes.
		for len(b) >= 8 {
			v := binary.LittleEndian.Uint64(b)
			binary.LittleEndian.PutUint64(b, v^key64)
			b = b[8:]
		}
	}

	// Then we xor until b is less than 4 bytes.
	for len(b) >= 4 {
		v := binary.LittleEndian.Uint32(b)
		binary.LittleEndian.PutUint32(b, v^key)
		b = b[4:]
	}

	// xor remaining bytes.
	for i := range b {
		b[i] ^= byte(key)
		key = bits.RotateLeft32(key, -8)
	}

	return key
}
