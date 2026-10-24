//go:build !js

package websocket

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/coder/websocket/internal/test/assert"
	"github.com/coder/websocket/internal/test/xrand"
)

type bufConn struct {
	bytes.Buffer
}

func (*bufConn) Close() error {
	return nil
}

// TestWritePreparedWire verifies that WritePrepared puts the same bytes on the
// wire as Write, including for messages written after it, and that the
// shared compressed payload is written only when the connection compresses
// without context takeover and compression makes the message smaller.
func TestWritePreparedWire(t *testing.T) {
	t.Parallel()

	largeMsg := strings.Repeat("prepared message ", 100)
	smallMsg := "small"
	nextMsg := []byte(strings.Repeat("next message ", 100))
	// Random bytes do not compress, so compression makes them larger.
	incompressibleMsg := string(xrand.Bytes(1024))

	testCases := []struct {
		name  string
		copts *compressionOptions
		msg   string
		// Whether the shared compressed payload is written. It is false both
		// when the regular write path is used and when the message is written
		// as is because compression does not make it smaller.
		wantCompressed bool
	}{
		{"Disabled", nil, largeMsg, false},
		{"ContextTakeover/AboveThreshold", CompressionContextTakeover.opts(), largeMsg, false},
		{"ContextTakeover/BelowThreshold", CompressionContextTakeover.opts(), smallMsg, false},
		{"NoContextTakeover/AboveThreshold", CompressionNoContextTakeover.opts(), largeMsg, true},
		{"NoContextTakeover/AtThreshold", CompressionNoContextTakeover.opts(), strings.Repeat("a", 512), true},
		{"NoContextTakeover/BelowThreshold", CompressionNoContextTakeover.opts(), strings.Repeat("a", 511), false},
		{"ContextTakeover/Incompressible", CompressionContextTakeover.opts(), incompressibleMsg, false},
		{"NoContextTakeover/Incompressible", CompressionNoContextTakeover.opts(), incompressibleMsg, false},
		// Only the server side of the negotiated options applies to server writes.
		{"ServerNoContextTakeover", &compressionOptions{serverNoContextTakeover: true}, largeMsg, true},
		{"ClientNoContextTakeover", &compressionOptions{clientNoContextTakeover: true}, largeMsg, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			newServerConn := func() (*Conn, *bufConn) {
				rwc := &bufConn{}
				c := newConn(connConfig{
					rwc:   rwc,
					copts: tc.copts,
					br:    bufio.NewReader(rwc),
					bw:    bufio.NewWriter(rwc),
				})
				t.Cleanup(func() {
					_ = c.CloseNow()
				})
				return c, rwc
			}

			ctx := context.Background()

			data := []byte(tc.msg)
			pm := NewPreparedMessage(MessageText, data)
			copy(data, "modified") // NewPreparedMessage must copy data.

			want, wantBuf := newServerConn()
			got, gotBuf := newServerConn()
			for range 2 {
				assert.Success(t, want.Write(ctx, MessageText, []byte(tc.msg)))
				assert.Success(t, want.Write(ctx, MessageBinary, nextMsg))

				assert.Success(t, got.WritePrepared(ctx, pm))
				assert.Success(t, got.Write(ctx, MessageBinary, nextMsg))
			}

			assert.Equal(t, "wire bytes", wantBuf.Bytes(), gotBuf.Bytes())
			assert.Equal(t, "compressed", tc.wantCompressed, pm.compressed)
		})
	}
}
