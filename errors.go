package websocket

import (
	"errors"
)

// ErrMessageTooBig is returned when a message exceeds the read limit.
var ErrMessageTooBig = errors.New("websocket: message too big")

// ErrCloseReasonTruncated indicates that Close shortened the reason to fit
// 123 bytes after replacing invalid UTF-8.
var ErrCloseReasonTruncated = errors.New("websocket: close reason truncated")
