package ws

import (
	"encoding/binary"
	"errors"
	"io"
	"math/bits"
)

type StatusCode int

const (
	StatusNormalClosure StatusCode = 1000 + iota
	StatusGoingAway
	StatusProtocolError
	StatusUnsupportedData
	// ...
)

func ParseClosePayload(p []byte) (code StatusCode, reason string, err error) {
	if len(p) < 2 {
		return 0, "", errors.New("close payload is less than 2 bytes")
	}

	code = StatusCode(binary.BigEndian.Uint16(p))
	reason = string(p[2:])

	return code, reason, nil
}

func WriteClosePayload(w io.Writer, code StatusCode, reason string) (n int, err error) {
	if bits.Len(uint(code)) > 2 {
		return 0, errors.New("status code is larger than 2 bytes")
	}

	var b [2]byte
	binary.BigEndian.PutUint16(b[:], uint16(code))

	n1, err := w.Write(b[:])
	if err != nil {
		return 0, err
	}

	if len(reason) < 1 {
		return n1, err
	}

	n2, err := io.WriteString(w, reason)
	if err != nil {
		return 0, err
	}
	return n1 + n2, nil
}
