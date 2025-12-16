//go:build !js

// Package websocket implements the RFC 6455 WebSocket protocol.
//
// https://tools.ietf.org/html/rfc6455
//
// # Overview
//
// Use Dial to dial a WebSocket server.
//
// Use Accept to accept a WebSocket client.
//
// Conn represents the resulting WebSocket connection.
//
// The examples are the best way to understand how to correctly use the library.
//
// The wsjson subpackage contains helpers for JSON and protobuf messages.
//
// More documentation at https://github.com/coder/websocket.
//
// # HTTP/2
//
// The package supports WebSocket over HTTP/2 via the extended CONNECT
// protocol (RFC 8441).
//
// This functionality is currently opt-in and requires setting the Protocol
// option on the AcceptOptions or DialOptions. When not set, HTTP/1.1 is used.
//
// Server-side extended CONNECT functionality must currently be enabled by
// setting GODEBUG=http2xconnect=1, see https://github.com/golang/go/issues/53208.
//
// See internal/examples/http2 for a minimal example.
//
// # WebAssembly (Wasm)
//
// The client side supports compiling to WebAssembly (Wasm).
// It wraps the WebSocket browser API.
//
// See https://developer.mozilla.org/en-US/docs/Web/API/WebSocket
//
// Some important caveats to be aware of:
//
//   - Accept always errors out
//   - HTTPProtocol in DialOptions and AcceptOptions is no-op
//   - HTTPClient, HTTPHeader and CompressionMode in DialOptions are no-op
//   - *http.Response from Dial is &http.Response{} with a 101 status code on success
//   - Conn.Ping is no-op
//   - Conn.CloseNow is Close(StatusGoingAway, "")
package websocket // import "github.com/coder/websocket"
