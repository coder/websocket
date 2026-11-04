package websocket

import (
	"errors"
)

// ErrBadStatus is wrapped by the error from Dial when the final response
// that DialOptions.HTTPClient returns for the opening handshake has a
// status other than 101 Switching Protocols. See Dial for the response
// returned with it. If HTTPClient.CheckRedirect returns an error other than
// http.ErrUseLastResponse, the error from Dial does not wrap ErrBadStatus
// and the response is nil.
//
// On js/wasm, Dial never returns an error wrapping ErrBadStatus because
// the browser does not expose the handshake response.
var ErrBadStatus = errors.New("websocket: bad status")

// ErrMessageTooBig is returned when a message exceeds the read limit.
var ErrMessageTooBig = errors.New("websocket: message too big")
