package ws

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"
)

//go:generate stringer -type=StatusCode
type StatusCode int

const (
	StatusNormalClosure StatusCode = 1000 + iota
	StatusGoingAway
	StatusProtocolError
	StatusUnsupportedData
	// ...
)

type CloseError struct {
	Code   StatusCode
	Reason string
}

func (e CloseError) Error() string {
	return fmt.Sprintf("WebSocket closed with status = %s and reason = %q", e.Code, e.Reason)
}

func parseClosePayload(p []byte) (code StatusCode, reason string, err error) {
	if len(p) < 2 {
		return 0, "", errors.New("close payload is less than 2 bytes")
	}

	code = StatusCode(binary.BigEndian.Uint16(p))
	reason = string(p[2:])

	return code, reason, nil
}

func writeClosePayload(w io.Writer, code StatusCode, reason string) error {
	if bits.Len(uint(code)) > 2 {
		return errors.New("status code is larger than 2 bytes")
	}

	var b [2]byte
	binary.BigEndian.PutUint16(b[:], uint16(code))

	_, err := w.Write(b[:])
	if err != nil {
		return err
	}

	if len(reason) < 1 {
		return nil
	}

	_, err = io.WriteString(w, reason)
	return err
}
