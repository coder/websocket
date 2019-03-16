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

func (h Header) Bytes() []byte {
	panic("TODO")
}

func ReaderHeader(r io.Reader) []byte {
	panic("TODO")
}
