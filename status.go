package ws

import (
	"encoding/binary"
	"errors"
	"fmt"
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
	Reason []byte
}

func (e CloseError) Error() string {
	return fmt.Sprintf("WebSocket closed with status = %v and reason = %q", e.Code, string(e.Reason))
}

func parseClosePayload(p []byte) (code StatusCode, reason []byte, err error) {
	if len(p) < 2 {
		return 0, nil, fmt.Errorf("close payload too small, cannot even contain the 2 byte status code")
	}

	code = StatusCode(binary.BigEndian.Uint16(p))
	reason = p[2:]

	return code, reason, nil
}

func closePayload(code StatusCode, reason []byte) ([]byte, error) {
	if bits.Len(uint(code)) > 16 {
		return nil, errors.New("status code is larger than 2 bytes")
	}
	if code == StatusNoStatusRcvd || code == StatusAbnormalClosure {
		return nil, fmt.Errorf("status code %v cannot be set by applications", code)
	}

	buf := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(buf[:], uint16(code))
	copy(buf[2:], reason)
	return buf, nil
}
