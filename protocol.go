package websocket

import "fmt"

// Protocol selects the HTTP version used for the WebSocket handshake.
//
// This type is used by both the client (Dial) and the server (Accept).
//
// Defaults and compatibility:
// - The zero value is ProtocolHTTP1 to preserve existing behavior.
// - HTTP/2 is NOT enabled by default.
//
// Client (Dial) semantics:
// - ProtocolHTTP1: perform an HTTP/1.1 Upgrade handshake.
// - ProtocolHTTP2: perform an HTTP/2 extended CONNECT (RFC 8441) handshake.
// - ProtocolAcceptAny: not supported for clients.
//
// Server (Accept) semantics:
// - ProtocolHTTP1: accept only HTTP/1.1 Upgrade handshakes.
// - ProtocolHTTP2: accept only HTTP/2 extended CONNECT (RFC 8441) handshakes.
// - ProtocolAcceptAny: accept either HTTP/1.1 Upgrade or HTTP/2 extended CONNECT.
//
// For HTTP/2 client dialing, callers must supply an http.Client configured with
// an http2.Transport (from golang.org/x/net/http2).
//
// Experimental: This type is experimental and may change in the future.
type Protocol int

const (
	// ProtocolAcceptAny accepts either HTTP/1.1 Upgrade or HTTP/2 extended
	// CONNECT. Valid only for servers (Accept). This value is rejected by
	// clients (Dial).
	ProtocolAcceptAny Protocol = iota - 1

	// ProtocolHTTP1 selects HTTP/1.1 GET+Upgrade for the WebSocket handshake.
	// This is the default (zero value).
	ProtocolHTTP1

	// ProtocolHTTP2 selects HTTP/2 extended CONNECT (RFC 8441) for the handshake.
	ProtocolHTTP2
)

// String implements fmt.Stringer.
func (p Protocol) String() string {
	switch p {
	case ProtocolHTTP1:
		return "ProtocolHTTP1"
	case ProtocolHTTP2:
		return "ProtocolHTTP2"
	case ProtocolAcceptAny:
		return "ProtocolAcceptAny"
	default:
		return fmt.Sprintf("Protocol(%d)", p)
	}
}
