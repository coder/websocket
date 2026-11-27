//go:build !js

package websocket_test

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/internal/test/assert"
	"github.com/coder/websocket/internal/test/xrand"
)

type countingConn struct {
	net.Conn
	writes *atomic.Int64
}

func (c countingConn) Write(p []byte) (int, error) {
	c.writes.Add(1)
	return c.Conn.Write(p)
}

func dialCounting(t testing.TB, writeBufferSize int) (*websocket.Conn, *atomic.Int64) {
	t.Helper()

	serverDone := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(serverDone)
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		c.SetReadLimit(-1)
		for {
			typ, p, err := c.Read(context.Background())
			if err != nil {
				return
			}
			if err := c.Write(context.Background(), typ, p); err != nil {
				return
			}
		}
	}))
	t.Cleanup(s.Close)

	var writes atomic.Int64
	transport := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(transport.CloseIdleConnections)
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		conn, err := d.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return countingConn{Conn: conn, writes: &writes}, nil
	}
	c, _, err := websocket.Dial(t.Context(), s.URL, &websocket.DialOptions{
		HTTPClient:      &http.Client{Transport: transport},
		WriteBufferSize: writeBufferSize,
	})
	assert.Success(t, err)
	c.SetReadLimit(-1)
	t.Cleanup(func() {
		c.CloseNow()
		<-serverDone // httptest.Server.Close does not wait for hijacked connections.
	})
	return c, &writes
}

func TestDialWriteBufferSize(t *testing.T) {
	t.Parallel()

	const defaultMsgSize = 64 << 10
	tests := []struct {
		name            string
		writeBufferSize int
		messageSize     int
		maxWrites       int64
		minWrites       int64
	}{
		{
			name:            "default",
			writeBufferSize: 0,
			messageSize:     defaultMsgSize,
			minWrites:       defaultMsgSize / 4096,
		},
		{
			name:            "negative",
			writeBufferSize: -1,
			messageSize:     defaultMsgSize,
			minWrites:       defaultMsgSize / 4096,
		},
		{
			name:            "explicitDefault",
			writeBufferSize: 4096,
			messageSize:     defaultMsgSize,
			minWrites:       defaultMsgSize / 4096,
		},
		{
			name:            "large",
			writeBufferSize: 128 << 10,
			messageSize:     defaultMsgSize,
			maxWrites:       1,
		},
		{
			name:            "small",
			writeBufferSize: 1024,
			messageSize:     defaultMsgSize,
			minWrites:       defaultMsgSize / 1024,
		},
		// Smaller than a frame header and not a multiple of the 4-byte mask key.
		{
			name:            "tiny",
			writeBufferSize: 5,
			messageSize:     1024,
			minWrites:       1024 / 5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			c, writes := dialCounting(t, tt.writeBufferSize)
			msg := xrand.Bytes(tt.messageSize)

			before := writes.Load()
			assert.Success(t, c.Write(ctx, websocket.MessageBinary, msg))
			got := writes.Load() - before
			if tt.maxWrites > 0 && got > tt.maxWrites {
				t.Errorf("got %d writes for a %d byte message, want at most %d", got, tt.messageSize, tt.maxWrites)
			}
			if tt.minWrites > 0 && got < tt.minWrites {
				t.Errorf("got %d writes for a %d byte message, want at least %d", got, tt.messageSize, tt.minWrites)
			}

			typ, echoed, err := c.Read(ctx)
			assert.Success(t, err)
			assert.Equal(t, "message type", websocket.MessageBinary, typ)
			if !bytes.Equal(msg, echoed) {
				t.Fatal("echoed message differs from the one sent")
			}

			assert.Success(t, c.Write(ctx, websocket.MessageText, []byte("small")))
			_, echoed, err = c.Read(ctx)
			assert.Success(t, err)
			assert.Equal(t, "small message", "small", string(echoed))

			assert.Success(t, c.Close(websocket.StatusNormalClosure, ""))
		})
	}
}

func BenchmarkDialWriteBufferSize(b *testing.B) {
	const messageSize = 64 << 10
	benchmarks := []struct {
		name string
		size int
	}{
		{name: "default", size: 0},
		{name: "128KiB", size: 128 << 10},
	}
	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			c, writes := dialCounting(b, bm.size)
			msg := make([]byte, messageSize)

			b.SetBytes(messageSize)
			b.ReportAllocs()
			before := writes.Load()
			for b.Loop() {
				if err := c.Write(b.Context(), websocket.MessageBinary, msg); err != nil {
					b.Fatal(err)
				}
				if _, _, err := c.Read(b.Context()); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(writes.Load()-before)/float64(b.N), "writes/op")
		})
	}
}
