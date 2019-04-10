package websocket

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"unicode/utf8"

	"golang.org/x/xerrors"
)

// StatusCode represents a WebSocket status code.
type StatusCode int

//go:generate go run golang.org/x/tools/cmd/stringer -type=StatusCode

// These codes were retrieved from:
// https://www.iana.org/assignments/websocket/websocket.xhtml#close-code-number
const (
	StatusNormalClosure StatusCode = 1000 + iota
	StatusGoingAway
	StatusProtocolError
	StatusUnsupportedData
	_ // 1004 is reserved.
	StatusNoStatusRcvd
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

// CloseError represents an error from a WebSocket close frame.
// Methods on the Conn will only return this for a non normal close code.
type CloseError struct {
	Code   StatusCode
	Reason string
}

func (e CloseError) Error() string {
	return fmt.Sprintf("WebSocket closed with status = %v and reason = %q", e.Code, e.Reason)
}

func parseClosePayload(p []byte) (code StatusCode, reason string, err error) {
	if len(p) < 2 {
		return 0, "", fmt.Errorf("close payload too small, cannot even contain the 2 byte status code")
	}

	code = StatusCode(binary.BigEndian.Uint16(p))
	reason = string(p[2:])

	if !utf8.ValidString(reason) {
		return 0, "", xerrors.Errorf("invalid utf-8: %q", reason)
	}
	if !validCloseCode(code) {
		return 0, "", xerrors.Errorf("invalid code %v", code)
	}

	return code, reason, nil
}

// See http://www.iana.org/assignments/websocket/websocket.xhtml#close-code-number
// and https://tools.ietf.org/html/rfc6455#section-7.4.1
var validReceivedCloseCodes = map[StatusCode]bool{
	StatusNormalClosure:           true,
	StatusGoingAway:               true,
	StatusProtocolError:           true,
	StatusUnsupportedData:         true,
	StatusNoStatusRcvd:            false,
	StatusAbnormalClosure:         false,
	StatusInvalidFramePayloadData: true,
	StatusPolicyViolation:         true,
	StatusMessageTooBig:           true,
	StatusMandatoryExtension:      true,
	StatusInternalError:           true,
	StatusServiceRestart:          true,
	StatusTryAgainLater:           true,
	StatusTLSHandshake:            false,
}

func validCloseCode(code StatusCode) bool {
	return validReceivedCloseCodes[code] || (code >= 3000 && code <= 4999)
}

const maxControlFramePayload = 125

func closePayload(code StatusCode, reason string) ([]byte, error) {
	if len(reason) > maxControlFramePayload-2 {
		return nil, xerrors.Errorf("reason string max is %v but got %q with length %v", maxControlFramePayload-2, reason, len(reason))
	}
	if bits.Len(uint(code)) > 16 {
		return nil, errors.New("status code is larger than 2 bytes")
	}
	if !validCloseCode(code) {
		return nil, fmt.Errorf("status code %v cannot be set", code)
	}

	buf := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(buf[:], uint16(code))
	copy(buf[2:], reason)
	return buf, nil
}
