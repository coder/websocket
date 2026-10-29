package websocket

import (
	"errors"
)

// ErrBadStatus is wrapped by the error from Dial when the server answers
// the opening handshake with an HTTP status other than 101 Switching
// Protocols. See Dial for the response returned with it.
//
// On js/wasm, Dial never returns an error wrapping ErrBadStatus because
// the browser does not expose the handshake response.
var ErrBadStatus = errors.New("websocket: bad status")

// ErrMessageTooBig is returned when a message exceeds the read limit.
var ErrMessageTooBig = errors.New("websocket: message too big")
