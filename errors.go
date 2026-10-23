package websocket

import (
	"errors"
)

// ErrMessageTooBig is returned when a message exceeds the read limit.
var ErrMessageTooBig = errors.New("websocket: message too big")

// ErrCloseReasonTruncated is returned by Close when the reason, after invalid
// UTF-8 was replaced, was longer than 123 bytes and Close shortened it.
var ErrCloseReasonTruncated = errors.New("websocket: close reason truncated")
