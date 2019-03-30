package ws

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
)

// DialOption represents a dial option that can be passed to Dial.
type DialOption interface {
	dialOption()
	fmt.Stringer
}

// DialHTTPClient is the http client used for the handshake.
// Its Transport must use HTTP/1.1 and must return writable bodies
// for WebSocket handshakes.
// http.Transport does this correctly.
func DialHTTPClient(h *http.Client) DialOption {
	panic("TODO")
}

// DialHeader are the HTTP headers included in the handshake request.
func DialHeader(h http.Header) DialOption {
	panic("TODO")
}

// DialSubprotocols accepts a slice of protcols to include in the Sec-WebSocket-Protocol header.
func DialSubprotocols(subprotocols ...string) DialOption {
	panic("TODO")
}

// We use this key for all client requests as the Sec-WebSocket-Key header is useless.
// See https://stackoverflow.com/a/37074398/4283659.
// We also use the same mask key for every message as it too does not make a difference.
var secWebSocketKey = base64.StdEncoding.EncodeToString(make([]byte, 16))

// Dial performs a WebSocket handshake on the given url with the given options.
func Dial(ctx context.Context, u string, opts ...DialOption) (*Conn, *http.Response, error) {
	panic("TODO")
}
