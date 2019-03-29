package wscore

import (
	"io"
)

// Header represents a WebSocket frame header.
// See https://tools.ietf.org/html/rfc6455#section-5.2
type Header struct {
	Fin    bool
	Rsv1   bool
	Rsv2   bool
	Rsv3   bool
	Opcode Opcode

	PayloadLength int64

	Masked  bool
	MaskKey [4]byte
}

// Bytes returns the bytes of the header.
func (h Header) Bytes() []byte {
	panic("TODO")
}

// ReadHeader reads a header from the reader.
func ReadHeader(r io.Reader) []byte {
	panic("TODO")
}
