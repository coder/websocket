//go:build !js

package websocket_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/internal/test/assert"
)

type countingConn struct {
	net.Conn
	writes *atomic.Int64
}

func (c countingConn) Write(p []byte) (int, error) {
	c.writes.Add(1)
	return c.Conn.Write(p)
}

func dialCounting(t testing.TB, ctx context.Context, writeBufferSize int) (*websocket.Conn, *atomic.Int64) {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		c.SetReadLimit(-1)
		for {
			typ, p, err := c.Read(ctx)
			if err != nil {
				return
			}
			if err := c.Write(ctx, typ, p); err != nil {
				return
			}
		}
	}))
	t.Cleanup(s.Close)

	writes := &atomic.Int64{}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(transport.CloseIdleConnections)
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return countingConn{Conn: conn, writes: writes}, nil
	}
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.URL, "http"), &websocket.DialOptions{
		HTTPClient:      &http.Client{Transport: transport},
		WriteBufferSize: writeBufferSize,
	})
	assert.Success(t, err)
	c.SetReadLimit(-1)
	t.Cleanup(func() { c.CloseNow() })
	return c, writes
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
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c, writes := dialCounting(t, ctx, tc.writeBufferSize)

			msg := make([]byte, tc.messageSize)
			_, err := rand.Read(msg)
			assert.Success(t, err)

			before := writes.Load()
			err = c.Write(ctx, websocket.MessageBinary, msg)
			assert.Success(t, err)
			got := writes.Load() - before

			if tc.maxWrites > 0 && got > tc.maxWrites {
				t.Fatalf("%d writes for a %d byte message, want at most %d", got, tc.messageSize, tc.maxWrites)
			}
			if tc.minWrites > 0 && got < tc.minWrites {
				t.Fatalf("%d writes for a %d byte message, want at least %d", got, tc.messageSize, tc.minWrites)
			}

			typ, echoed, err := c.Read(ctx)
			assert.Success(t, err)
			assert.Equal(t, "message type", websocket.MessageBinary, typ)
			if !bytes.Equal(msg, echoed) {
				t.Fatal("echoed message differs from the one sent")
			}

			err = c.Write(ctx, websocket.MessageText, []byte("small"))
			assert.Success(t, err)
			_, echoed, err = c.Read(ctx)
			assert.Success(t, err)
			assert.Equal(t, "small message", "small", string(echoed))

			assert.Success(t, c.Close(websocket.StatusNormalClosure, ""))
		})
	}
}

func BenchmarkDialWriteBufferSize(b *testing.B) {
	const messageSize = 64 << 10
	for _, size := range []int{0, 128 << 10} {
		name := "default"
		if size > 0 {
			name = "128KiB"
		}
		b.Run(name, func(b *testing.B) {
			ctx := b.Context()
			c, writes := dialCounting(b, ctx, size)
			msg := make([]byte, messageSize)

			b.SetBytes(messageSize)
			b.ReportAllocs()
			b.ResetTimer()
			before := writes.Load()
			for range b.N {
				if err := c.Write(ctx, websocket.MessageBinary, msg); err != nil {
					b.Fatal(err)
				}
				if _, _, err := c.Read(ctx); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(writes.Load()-before)/float64(b.N), "writes/op")
		})
	}
}
