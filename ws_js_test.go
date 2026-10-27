package websocket_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/internal/test/assert"
	"github.com/coder/websocket/internal/test/wstest"
	"github.com/coder/websocket/internal/test/xrand"
)

func TestWasm(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c, resp, err := websocket.Dial(ctx, os.Getenv("WS_ECHO_SERVER_URL"), &websocket.DialOptions{
		Subprotocols: []string{"echo"},
	})
	assert.Success(t, err)
	defer c.Close(websocket.StatusInternalError, "")

	assert.Equal(t, "subprotocol", "echo", c.Subprotocol())
	assert.Equal(t, "response code", http.StatusSwitchingProtocols, resp.StatusCode)

	c.SetReadLimit(65536)
	for range 10 {
		err = wstest.Echo(ctx, c, 65536)
		assert.Success(t, err)
	}

	err = c.Close(websocket.StatusNormalClosure, "")
	assert.Success(t, err)
}

func TestWasmNetConnReadLimit(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		netConn func(ctx context.Context, c *websocket.Conn) net.Conn
		msgSize int
		wantErr error
	}{
		{"Unset", func(ctx context.Context, c *websocket.Conn) net.Conn {
			return websocket.NetConn(ctx, c, websocket.MessageBinary)
		}, 65536, nil},
		{"DisabledBeforeNetConn", func(ctx context.Context, c *websocket.Conn) net.Conn {
			c.SetReadLimit(-1)
			return websocket.NetConn(ctx, c, websocket.MessageBinary)
		}, 65536, nil},
		{"BeforeNetConn", func(ctx context.Context, c *websocket.Conn) net.Conn {
			c.SetReadLimit(1024)
			return websocket.NetConn(ctx, c, websocket.MessageBinary)
		}, 4096, websocket.ErrMessageTooBig},
		{"AfterNetConn", func(ctx context.Context, c *websocket.Conn) net.Conn {
			nc := websocket.NetConn(ctx, c, websocket.MessageBinary)
			c.SetReadLimit(1024)
			return nc
		}, 4096, websocket.ErrMessageTooBig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()

			c, _, err := websocket.Dial(ctx, os.Getenv("WS_ECHO_SERVER_URL"), &websocket.DialOptions{
				Subprotocols: []string{"echo"},
			})
			assert.Success(t, err)
			// The echo server expects StatusNormalClosure.
			defer c.Close(websocket.StatusNormalClosure, "")

			nc := tc.netConn(ctx, c)
			msg := []byte(xrand.String(tc.msgSize))
			_, err = nc.Write(msg)
			assert.Success(t, err)

			got := make([]byte, len(msg))
			_, err = io.ReadFull(nc, got)
			if tc.wantErr != nil {
				assert.ErrorIs(t, tc.wantErr, err)
				return
			}
			assert.Success(t, err)
			assert.Equal(t, "read msg", msg, got)
		})
	}
}

func TestWasmDialTimeout(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	beforeDial := time.Now()
	_, _, err := websocket.Dial(ctx, "ws://example.com:9893", &websocket.DialOptions{
		Subprotocols: []string{"echo"},
	})
	assert.Error(t, err)
	if time.Since(beforeDial) >= time.Second {
		t.Fatal("wasm context dial timeout is not working", time.Since(beforeDial))
	}
}
