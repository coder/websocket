package websocket

import "fmt"

// HTTPProtocol selects the HTTP version used for the WebSocket handshake.
//
// This type is used by both the client (Dial) and the server (Accept).
//
// Defaults and compatibility:
// - The zero value is HTTPProtocol1 to preserve existing behavior.
// - HTTP/2 is NOT enabled by default.
//
// Client (Dial) semantics:
// - HTTPProtocol1: perform an HTTP/1.1 Upgrade handshake.
// - HTTPProtocol2: perform an HTTP/2 extended CONNECT (RFC 8441) handshake.
// - HTTPProtocolAny: not supported for clients.
//
// Server (Accept) semantics:
// - HTTPProtocol1: accept only HTTP/1.1 Upgrade handshakes.
// - HTTPProtocol2: accept only HTTP/2 extended CONNECT (RFC 8441) handshakes.
// - HTTPProtocolAny: accept either HTTP/1.1 Upgrade or HTTP/2 extended CONNECT.
//
// For HTTP/2 client dialing, callers must supply an http.Client configured with
// an http2.Transport (from golang.org/x/net/http2).
//
// Experimental: This type is experimental and may change in the future.
type HTTPProtocol int

const (
	// HTTPProtocolAny accepts either HTTP/1.1 Upgrade or HTTP/2 extended
	// CONNECT. Valid only for servers (Accept). This value is rejected by
	// clients (Dial).
	HTTPProtocolAny HTTPProtocol = iota - 1

	// HTTPProtocol1 selects HTTP/1.1 GET+Upgrade for the WebSocket handshake.
	// This is the default (zero value).
	HTTPProtocol1

	// HTTPProtocol2 selects HTTP/2 extended CONNECT (RFC 8441) for the handshake.
	HTTPProtocol2
)

// String implements fmt.Stringer.
func (p HTTPProtocol) String() string {
	switch p {
	case HTTPProtocol1:
		return "HTTPProtocol1"
	case HTTPProtocol2:
		return "HTTPProtocol2"
	case HTTPProtocolAny:
		return "HTTPProtocolAny"
	default:
		return fmt.Sprintf("HTTPProtocol(%d)", p)
	}
}
