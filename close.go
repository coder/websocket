package websocket

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"nhooyr.io/websocket/internal/wsframe"
)

// StatusCode represents a WebSocket status code.
// https://tools.ietf.org/html/rfc6455#section-7.4
type StatusCode int

// These codes were retrieved from:
// https://www.iana.org/assignments/websocket/websocket.xhtml#close-code-number
//
// The defined constants only represent the status codes registered with IANA.
// The 4000-4999 range of status codes is reserved for arbitrary use by applications.
const (
	StatusNormalClosure   StatusCode = 1000
	StatusGoingAway       StatusCode = 1001
	StatusProtocolError   StatusCode = 1002
	StatusUnsupportedData StatusCode = 1003

	// 1004 is reserved and so not exported.
	statusReserved StatusCode = 1004

	// StatusNoStatusRcvd cannot be sent in a close message.
	// It is reserved for when a close message is received without
	// an explicit status.
	StatusNoStatusRcvd StatusCode = 1005

	// StatusAbnormalClosure is only exported for use with Wasm.
	// In non Wasm Go, the returned error will indicate whether the connection was closed or not or what happened.
	StatusAbnormalClosure StatusCode = 1006

	StatusInvalidFramePayloadData StatusCode = 1007
	StatusPolicyViolation         StatusCode = 1008
	StatusMessageTooBig           StatusCode = 1009
	StatusMandatoryExtension      StatusCode = 1010
	StatusInternalError           StatusCode = 1011
	StatusServiceRestart          StatusCode = 1012
	StatusTryAgainLater           StatusCode = 1013
	StatusBadGateway              StatusCode = 1014

	// StatusTLSHandshake is only exported for use with Wasm.
	// In non Wasm Go, the returned error will indicate whether there was a TLS handshake failure.
	StatusTLSHandshake StatusCode = 1015
)

// CloseError represents a WebSocket close frame.
// It is returned by Conn's methods when a WebSocket close frame is received from
// the peer.
// You will need to use the https://golang.org/pkg/errors/#As function, new in Go 1.13,
// to check for this error. See the CloseError example.
type CloseError struct {
	Code   StatusCode
	Reason string
}

func (ce CloseError) Error() string {
	return fmt.Sprintf("status = %v and reason = %q", ce.Code, ce.Reason)
}

// CloseStatus is a convenience wrapper around errors.As to grab
// the status code from a *CloseError. If the passed error is nil
// or not a *CloseError, the returned StatusCode will be -1.
func CloseStatus(err error) StatusCode {
	var ce CloseError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return -1
}

func parseClosePayload(p []byte) (CloseError, error) {
	if len(p) == 0 {
		return CloseError{
			Code: StatusNoStatusRcvd,
		}, nil
	}

	code, reason, err := wsframe.ParseClosePayload(p)
	if err != nil {
		return CloseError{}, err
	}

	ce := CloseError{
		Code:   StatusCode(code),
		Reason: reason,
	}

	if !validWireCloseCode(ce.Code) {
		return CloseError{}, fmt.Errorf("invalid status code %v", ce.Code)
	}

	return ce, nil
}

// See http://www.iana.org/assignments/websocket/websocket.xhtml#close-code-number
// and https://tools.ietf.org/html/rfc6455#section-7.4.1
func validWireCloseCode(code StatusCode) bool {
	switch code {
	case statusReserved, StatusNoStatusRcvd, StatusAbnormalClosure, StatusTLSHandshake:
		return false
	}

	if code >= StatusNormalClosure && code <= StatusBadGateway {
		return true
	}
	if code >= 3000 && code <= 4999 {
		return true
	}

	return false
}

func (ce CloseError) bytes() ([]byte, error) {
	// TODO move check into frame write
	if len(ce.Reason) > wsframe.MaxControlFramePayload-2 {
		return nil, fmt.Errorf("reason string max is %v but got %q with length %v", wsframe.MaxControlFramePayload-2, ce.Reason, len(ce.Reason))
	}
	if !validWireCloseCode(ce.Code) {
		return nil, fmt.Errorf("status code %v cannot be set", ce.Code)
	}

	buf := make([]byte, 2+len(ce.Reason))
	binary.BigEndian.PutUint16(buf, uint16(ce.Code))
	copy(buf[2:], ce.Reason)
	return buf, nil
}

// CloseRead will start a goroutine to read from the connection until it is closed or a data message
// is received. If a data message is received, the connection will be closed with StatusPolicyViolation.
// Since CloseRead reads from the connection, it will respond to ping, pong and close frames.
// After calling this method, you cannot read any data messages from the connection.
// The returned context will be cancelled when the connection is closed.
//
// Use this when you do not want to read data messages from the connection anymore but will
// want to write messages to it.
func (c *Conn) CloseRead(ctx context.Context) context.Context {
	c.isReadClosed.Store(1)

	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer cancel()
		// We use the unexported reader method so that we don't get the read closed error.
		c.reader(ctx, true)
		// Either the connection is already closed since there was a read error
		// or the context was cancelled or a message was read and we should close
		// the connection.
		c.Close(StatusPolicyViolation, "unexpected data message")
	}()
	return ctx
}

// SetReadLimit sets the max number of bytes to read for a single message.
// It applies to the Reader and Read methods.
//
// By default, the connection has a message read limit of 32768 bytes.
//
// When the limit is hit, the connection will be closed with StatusMessageTooBig.
func (c *Conn) SetReadLimit(n int64) {
	c.msgReadLimit.Store(n)
}

func (c *Conn) setCloseErr(err error) {
	c.closeErrOnce.Do(func() {
		c.closeErr = fmt.Errorf("websocket closed: %w", err)
	})
}

func (c *Conn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}
