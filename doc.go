// +build !js

// Package websocket is a minimal and idiomatic implementation of the WebSocket protocol.
//
// https://tools.ietf.org/html/rfc6455
//
// Conn, Dial, and Accept are the main entrypoints into this package. Use Dial to dial
// a WebSocket server, Accept to accept a WebSocket client dial and then Conn to interact
// with the resulting WebSocket connections.
//
// The examples are the best way to understand how to correctly use the library.
//
// The wsjson and wspb subpackages contain helpers for JSON and ProtoBuf messages.
//
// See https://nhooyr.io/websocket for more overview docs and a
// comparison with existing implementations.
//
// Use the errors.As function new in Go 1.13 to check for websocket.CloseError.
// Or use the CloseStatus function to grab the StatusCode out of a websocket.CloseError
// See the CloseStatus example.
//
// Wasm
//
// The client side fully supports compiling to Wasm.
// It wraps the WebSocket browser API.
//
// See https://developer.mozilla.org/en-US/docs/Web/API/WebSocket
//
// Thus the unsupported features (not compiled in) for Wasm are:
//
//  - Accept and AcceptOptions
//  - Conn.Ping
//  - HTTPClient and HTTPHeader fields in DialOptions
//
// The *http.Response returned by Dial will always either be nil or &http.Response{} as
// we do not have access to the handshake response in the browser.
//
// The Writer method on the Conn buffers everything in memory and then sends it as a message
// when the writer is closed.
//
// The Reader method also reads the entire response and then returns a reader that
// reads from the byte slice.
//
// SetReadLimit cannot actually limit the number of bytes read from the connection so instead
// when a message beyond the limit is fully read, it throws an error.
//
// Writes are also always async so the passed context is no-op.
//
// Everything else is fully supported. This includes the wsjson and wspb helper packages.
//
// Once https://github.com/gopherjs/gopherjs/issues/929 is closed, GopherJS should be supported
// as well.
package websocket // import "nhooyr.io/websocket"
