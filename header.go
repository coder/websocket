package ws

import (
	"io"
)

// header represents a WebSocket frame header.
// See https://tools.ietf.org/html/rfc6455#section-5.2
// The fields are exported for easy printing for debugging.
type header struct {
	Fin    bool
	Rsv1   bool
	Rsv2   bool
	Rsv3   bool
	Opcode opcode

	PayloadLength int64

	Masked  bool
	MaskKey [4]byte
}

// Bytes returns the bytes of the header.
func (h header) Bytes() []byte {
	panic("TODO")
}

// ReadHeader reads a header from the reader.
func ReadHeader(r io.Reader) []byte {
	panic("TODO")
}
