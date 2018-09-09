package wscore

import (
	"io"
)

// This is how it is capitalized in the RFC.
type Opcode int

const (
	Continuation Opcode = iota
	Text
	Binary
	// 3 - 7 are reserved for further non-control frames.
	Close = 8
	Ping
	Pong
	// 11-16 are reserved for further control frames.
)

type Header struct {
	FIN bool

	RSV1 bool
	RSV2 bool
	RSV3 bool

	Opcode Opcode

	// Length is an integer because the RFC mandates the MSB bit cannot be set.
	// We cannot send or receive a frame with negative length.
	Length int64

	Mask []byte
}

func (f *Header) Bytes() []byte {
	panic("TODO")
}

func (f *Header) MaskPayload(payload []byte) {
	panic("TODO")
}

func ReadHeader(w io.Writer) (*Header, error) {
	panic("TODO")
}

type StatusCode uint16

const (
	NormalClosure StatusCode = 1000 + iota
	GoingAway
	ProtocolError
	UnsupportedData
	// TODO
)

func SecWebsocketAccept(secWebsocketKey string) string {
	panic("TODO")
}
