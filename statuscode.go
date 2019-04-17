package websocket

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"

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
	// statusAbnormalClosure is unexported because it isn't necessary, at least until WASM.
	// The error returned will indicate whether the connection was closed or not or what happened.
	// It only makes sense for browser clients.
	statusAbnormalClosure
	StatusInvalidFramePayloadData
	StatusPolicyViolation
	StatusMessageTooBig
	StatusMandatoryExtension
	StatusInternalError
	StatusServiceRestart
	StatusTryAgainLater
	StatusBadGateway
	// statusTLSHandshake is unexported because we just return
	// handshake error in dial. We do not return a conn
	// so there is nothing to use this on. At least until WASM.
	statusTLSHandshake
)

// CloseError represents an error from a WebSocket close frame.
// Methods on the Conn will only return this for a non normal close code.
type CloseError struct {
	Code   StatusCode
	Reason string
}

func (ce CloseError) Error() string {
	return fmt.Sprintf("WebSocket closed with status = %v and reason = %q", ce.Code, ce.Reason)
}

func parseClosePayload(p []byte) (CloseError, error) {
	if len(p) == 0 {
		return CloseError{
			Code: StatusNoStatusRcvd,
		}, nil
	}

	if len(p) < 2 {
		return CloseError{}, fmt.Errorf("close payload too small, cannot even contain the 2 byte status code")
	}

	ce := CloseError{
		Code:   StatusCode(binary.BigEndian.Uint16(p)),
		Reason: string(p[2:]),
	}

	if !validWireCloseCode(ce.Code) {
		return CloseError{}, xerrors.Errorf("invalid code %v", ce.Code)
	}

	return ce, nil
}

// See http://www.iana.org/assignments/websocket/websocket.xhtml#close-code-number
// and https://tools.ietf.org/html/rfc6455#section-7.4.1
func validWireCloseCode(code StatusCode) bool {
	if code >= StatusNormalClosure && code <= statusTLSHandshake {
		switch code {
		case 1004, StatusNoStatusRcvd, statusAbnormalClosure, statusTLSHandshake:
			return false
		default:
			return true
		}
	}
	if code >= 3000 && code <= 4999 {
		return true
	}

	return false
}

const maxControlFramePayload = 125

func (ce CloseError) bytes() ([]byte, error) {
	if len(ce.Reason) > maxControlFramePayload-2 {
		return nil, xerrors.Errorf("reason string max is %v but got %q with length %v", maxControlFramePayload-2, ce.Reason, len(ce.Reason))
	}
	if bits.Len(uint(ce.Code)) > 16 {
		return nil, errors.New("status code is larger than 2 bytes")
	}
	if !validWireCloseCode(ce.Code) {
		return nil, fmt.Errorf("status code %v cannot be set", ce.Code)
	}

	buf := make([]byte, 2+len(ce.Reason))
	binary.BigEndian.PutUint16(buf[:], uint16(ce.Code))
	copy(buf[2:], ce.Reason)
	return buf, nil
}
