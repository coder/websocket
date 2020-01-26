// +build !js

package websocket_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cdr.dev/slog/sloggers/slogtest/assert"

	"nhooyr.io/websocket"
)

func TestConn(t *testing.T) {
	t.Parallel()

	t.Run("json", func(t *testing.T) {
		s, closeFn := testServer(t, func(w http.ResponseWriter, r *http.Request) {
			c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
				Subprotocols:       []string{"echo"},
				InsecureSkipVerify: true,
				CompressionOptions: websocket.CompressionOptions{
					Mode: websocket.CompressionNoContextTakeover,
				},
			})
			assert.Success(t, "accept", err)
			defer c.Close(websocket.StatusInternalError, "")

			err = echoLoop(r.Context(), c)
			assertCloseStatus(t, websocket.StatusNormalClosure, err)
		}, false)
		defer closeFn()

		wsURL := strings.Replace(s.URL, "http", "ws", 1)

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		opts := &websocket.DialOptions{
			Subprotocols: []string{"echo"},
			CompressionOptions: websocket.CompressionOptions{
				Mode: websocket.CompressionNoContextTakeover,
			},
		}
		opts.HTTPClient = s.Client()

		c, _, err := websocket.Dial(ctx, wsURL, opts)
		assert.Success(t, "dial", err)
		assertJSONEcho(t, ctx, c, 2)
	})
}

func testServer(tb testing.TB, fn func(w http.ResponseWriter, r *http.Request), tls bool) (s *httptest.Server, closeFn func()) {
	h := http.HandlerFunc(fn)
	if tls {
		s = httptest.NewTLSServer(h)
	} else {
		s = httptest.NewServer(h)
	}
	closeFn2 := wsgrace(s.Config)
	return s, func() {
		err := closeFn2()
		if err != nil {
			tb.Fatal(err)
		}
	}
}

// grace wraps s.Handler to gracefully shutdown WebSocket connections.
// The returned function must be used to close the server instead of s.Close.
func wsgrace(s *http.Server) (closeFn func() error) {
	h := s.Handler
	var conns int64
	s.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&conns, 1)
		defer atomic.AddInt64(&conns, -1)

		ctx, cancel := context.WithTimeout(r.Context(), time.Second*5)
		defer cancel()

		r = r.WithContext(ctx)

		h.ServeHTTP(w, r)
	})

	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		err := s.Shutdown(ctx)
		if err != nil {
			return fmt.Errorf("server shutdown failed: %v", err)
		}

		t := time.NewTicker(time.Millisecond * 10)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if atomic.LoadInt64(&conns) == 0 {
					return nil
				}
			case <-ctx.Done():
				return fmt.Errorf("failed to wait for WebSocket connections: %v", ctx.Err())
			}
		}
	}
}

// echoLoop echos every msg received from c until an error
// occurs or the context expires.
// The read limit is set to 1 << 30.
func echoLoop(ctx context.Context, c *websocket.Conn) error {
	defer c.Close(websocket.StatusInternalError, "")

	c.SetReadLimit(1 << 30)

	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	b := make([]byte, 32<<10)
	for {
		typ, r, err := c.Reader(ctx)
		if err != nil {
			return err
		}

		w, err := c.Writer(ctx, typ)
		if err != nil {
			return err
		}

		_, err = io.CopyBuffer(w, r, b)
		if err != nil {
			return err
		}

		err = w.Close()
		if err != nil {
			return err
		}
	}
}
