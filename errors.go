package websocket

import (
	"errors"
)

// ErrBadStatus is wrapped by the error from Dial when the final response
// that DialOptions.HTTPClient returns for the opening handshake has a
// status other than 101 Switching Protocols. See Dial for the response
// returned with it. A redirect that HTTPClient.CheckRedirect refuses is
// returned as a request error with a nil response, so it does not wrap
// ErrBadStatus.
//
// On js/wasm, Dial never returns an error wrapping ErrBadStatus because
// the browser does not expose the handshake response.
var ErrBadStatus = errors.New("websocket: bad status")

// ErrMessageTooBig is returned when a message exceeds the read limit.
var ErrMessageTooBig = errors.New("websocket: message too big")
