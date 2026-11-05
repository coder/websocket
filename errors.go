package websocket

import (
	"errors"
)

// ErrBadStatus means that the server answered the opening handshake from
// Dial with a status other than 101 Switching Protocols. Test for it with
// errors.Is. For backward compatibility, the error from Dial does not
// unwrap to it, so its innermost error is still the status text.
var ErrBadStatus = errors.New("websocket: bad status")

// ErrMessageTooBig is returned when a message exceeds the read limit.
var ErrMessageTooBig = errors.New("websocket: message too big")
