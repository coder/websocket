package websocket

import (
	"errors"
)

// ErrBadStatus is wrapped by the error from Dial when the server answers
// the opening handshake with a status other than 101 Switching Protocols.
var ErrBadStatus = errors.New("websocket: bad status")

// ErrMessageTooBig is returned when a message exceeds the read limit.
var ErrMessageTooBig = errors.New("websocket: message too big")
