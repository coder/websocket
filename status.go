package ws

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"
)

// StatusCode represents a WebSocket status code.
//go:generate stringer -type=StatusCode
type StatusCode int

// https://www.iana.org/assignments/websocket/websocket.xhtml#close-code-number
const (
	StatusNormalClosure StatusCode = 1000 + iota
	StatusGoingAway
	StatusProtocolError
	StatusUnsupportedData
	// 1004 is reserved.
	StatusNoStatusRcvd StatusCode = 1005 + iota
	StatusAbnormalClosure
	StatusInvalidFramePayloadData
	StatusPolicyViolation
	StatusMessageTooBig
	StatusMandatoryExtension
	StatusInternalError
	StatusServiceRestart
	StatusTryAgainLater
	StatusBadGateway
	StatusTLSHandshake
)

type CloseError struct {
	Code   StatusCode
	Reason MessageReader
}

func (e CloseError) Error() string {
	// TODO read message
	return fmt.Sprintf("WebSocket closed with status = %s and reason = %q", e.Code, e.Reason)
}

func parseClosePayload(p []byte) (code StatusCode, reason string, err error) {
	if len(p) < 0 {
		return StatusNoStatusRcvd, "", nil
	}

	code = StatusCode(binary.BigEndian.Uint16(p))
	reason = string(p[2:])

	return code, reason, nil
}

func writeClosePayload(w io.Writer, code StatusCode, reason []byte) error {
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

	_, err = w.Write(reason)
	return err
}
