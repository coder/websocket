//go:build !js

package websocket

import (
	"bytes"
	"fmt"
	"sync"
)

// PreparedMessage is a message that can be written to many connections
// efficiently with Conn.WritePrepared, such as when broadcasting the same
// update to every subscriber.
//
// With compression enabled, most of the cost of writing a message is
// compressing it. Conn.Write compresses the message once per connection, while
// Conn.WritePrepared compresses it once and shares the compressed payload between
// connections. Sharing is only possible when all the following hold:
//
//   - The connection compresses without context takeover. For a server, this
//     is the case with CompressionNoContextTakeover, or with
//     CompressionContextTakeover when the client negotiates
//     server_no_context_takeover. For a client, client_no_context_takeover
//     plays the same role.
//   - The message is at least as long as the compression threshold, 512 bytes
//     by default when compressing without context takeover. Smaller messages
//     are not compressed.
//
// Otherwise, Conn.WritePrepared costs the same as Conn.Write. With context takeover,
// each connection compresses against the sliding window of its own previous
// messages, so the compressed payload differs per connection and cannot be
// shared. Without compression there is no expensive work to share. Clients
// generate a new masking key for every write in all cases.
//
// The compressed payload is computed on the first write that needs it and
// kept for the lifetime of the PreparedMessage, so a PreparedMessage can be
// written any number of times, for example a greeting sent to every new
// connection. It is safe for concurrent use.
//
// In Wasm, the browser handles framing and compression, so Conn.WritePrepared is
// equivalent to Conn.Write.
type PreparedMessage struct {
	typ  MessageType
	data []byte

	// Written to connections that compress without context takeover.
	compressOnce sync.Once
	deflated     []byte // Compressed data, set only if smaller than data.
	compressed   bool   // Whether deflated is written instead of data.
	compressErr  error
}

// NewPreparedMessage returns a PreparedMessage of the specified type with cloned data.
func NewPreparedMessage(typ MessageType, data []byte) *PreparedMessage {
	return &PreparedMessage{
		typ:  typ,
		data: bytes.Clone(data),
	}
}

// compress returns the bytes to write on connections that compress without
// context takeover and whether they are compressed. They are the original
// data if compression does not make it smaller.
func (pm *PreparedMessage) compress() ([]byte, bool, error) {
	pm.compressOnce.Do(func() {
		var buf bytes.Buffer

		tw := &trimLastFourBytesWriter{
			w: &buf,
		}
		fw := getFlateWriter(tw)
		defer putFlateWriter(fw)

		_, err := fw.Write(pm.data)
		if err != nil {
			pm.compressErr = fmt.Errorf("failed to compress: %w", err)
			return
		}

		err = fw.Flush()
		if err != nil {
			pm.compressErr = fmt.Errorf("failed to flush compression: %w", err)
			return
		}

		if buf.Len() < len(pm.data) {
			pm.deflated, pm.compressed = buf.Bytes(), true
		}
	})
	if !pm.compressed {
		return pm.data, false, pm.compressErr
	}
	return pm.deflated, true, nil
}
