package websocket

import (
	"context"
)

const (
	secWebSocketProtocol = "Sec-WebSocket-Protocol"
)

// Conn represents a WebSocket connection.
type Conn struct{}

// Subprotocol returns the negotiated subprotocol.
// An empty string means the default protocol.
func (c *Conn) Subprotocol() string {
	panic("TODO")
}

// MessageWriter returns a writer bounded by the context that will write
// a WebSocket data frame of type dataType to the connection.
// Ensure you close the MessageWriter once you have written to entire message.
// Concurrent calls to MessageWriter are ok.
func (c *Conn) MessageWriter(dataType DataType) *MessageWriter {
	panic("TODO")
}

// ReadMessage will wait until there is a WebSocket data frame to read from the connection.
// It returns the type of the data, a reader to read it and also an error.
// Please use SetContext on the reader to bound the read operation.
func (c *Conn) ReadMessage(ctx context.Context) (DataType, *MessageReader, error) {
	panic("TODO")
}

// Close closes the WebSocket connection with the given status code and reason.
// It will write a WebSocket close frame with a timeout of 5 seconds.
func (c *Conn) Close(code StatusCode, reason string) error {
	// This function also will not wait for a close frame from the peer like the RFC
	// wants because that makes no sense and I don't think anyone actually follows that.
	// Definitely worth seeing what popular browsers do later.
	panic("TODO")
}

// MessageWriter enables writing to a WebSocket connection.
// Ensure you close the MessageWriter once you have written to entire message.
type MessageWriter struct{}

// Write writes the given bytes to the WebSocket connection.
// The frame will automatically be fragmented as appropriate
// with the buffers obtained from http.Hijacker.
// Please ensure you call Close once you have written the full message.
func (w *MessageWriter) Write(p []byte) (n int, err error) {
	panic("TODO")
}

// SetContext bounds the writer to the context.
// This must be called before any write.
func (w *MessageWriter) SetContext(ctx context.Context) {
	panic("TODO")
}

// Compress marks the message to be compressed.
// This must be called before any write.
func (w *MessageWriter) Compress() {
	panic("TODO")
}

// Close flushes the frame to the connection.
// This must be called for every MessageWriter.
func (w *MessageWriter) Close() error {
	panic("TODO")
}

// MessageReader enables reading a data frame from the WebSocket connection.
type MessageReader struct{}

// SetContext bounds the read operation to the ctx.
// By default, the context is the one passed to conn.ReadMessage.
// You still almost always want a separate context for reading the message though.
// Must be called before any read.
func (r *MessageReader) SetContext(ctx context.Context) {
	panic("TODO")
}

// Limit limits the number of bytes read by the reader.
// Must be called before any read.
func (r *MessageReader) Limit(bytes int) {
	panic("TODO")
}

// Read reads as many bytes as possible into p.
func (r *MessageReader) Read(p []byte) (n int, err error) {
	panic("TODO")
}
