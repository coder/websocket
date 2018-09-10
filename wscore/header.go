package wscore

import (
	"io"
)

type Opcode int

type Header struct {
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

func (f *Header) Bytes() []byte {
	panic("TODO")
}

func (f *Header) MaskPayload(payload []byte) {
	panic("TODO")
}

func ReadHeader(w io.Writer) (Header, error) {
	panic("TODO")
}
