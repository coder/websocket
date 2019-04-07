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
//go:generate go run golang.org/x/tools/cmd/stringer -type=StatusCode
type StatusCode int

// These codes were retrieved from:
// https://www.iana.org/assignments/websocket/websocket.xhtml#close-code-number
const (
	StatusNormalClosure StatusCode = 1000 + iota
	StatusGoingAway
	StatusProtocolError
	StatusUnsupportedData
	// 1004 is reserved.
	StatusNoStatusRcvd StatusCode = 1005 + iota - 4
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
	if !isValidReceivedCloseCode(code) {
		return 0, "", xerrors.Errorf("invalid code %v", code)
	}

	return code, reason, nil
}

var validReceivedCloseCodes = map[StatusCode]bool{
	// see http://www.iana.org/assignments/websocket/websocket.xhtml#close-code-number
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

func isValidReceivedCloseCode(code StatusCode) bool {
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
	if code == StatusNoStatusRcvd || code == StatusAbnormalClosure {
		return nil, fmt.Errorf("status code %v cannot be set by applications", code)
	}

	buf := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(buf[:], uint16(code))
	copy(buf[2:], reason)
	return buf, nil
}
