package websocket_test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/internal/test/assert"
	"github.com/coder/websocket/internal/test/wstest"
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

func TestWasmCloseReasonTruncated(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c, _, err := websocket.Dial(ctx, os.Getenv("WS_ECHO_SERVER_URL"), &websocket.DialOptions{
		Subprotocols: []string{"echo"},
	})
	assert.Success(t, err)

	// The echo server checks the close code.
	err = c.Close(websocket.StatusNormalClosure, strings.Repeat("é", 62))
	assert.ErrorIs(t, websocket.ErrCloseReasonTruncated, err)
	assert.Equal(t, "close error", "websocket: close reason truncated from 124 to 122 bytes", err.Error())
}

func TestWasmCloseReasonInvalidUTF8(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c, _, err := websocket.Dial(ctx, os.Getenv("WS_ECHO_SERVER_URL"), &websocket.DialOptions{
		Subprotocols: []string{"echo"},
	})
	assert.Success(t, err)

	// Without the U+FFFD replacement, these 50 bytes become more than 123 bytes
	// when converted to a JavaScript string. The echo server checks the close code.
	err = c.Close(websocket.StatusNormalClosure, strings.Repeat("\x80", 50))
	assert.Success(t, err)
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

func TestWasmNetConnZeroLengthRead(t *testing.T) {
	for _, buffered := range []bool{false, true} {
		name := "idle"
		if buffered {
			name = "buffered"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c, _, err := websocket.Dial(ctx, os.Getenv("WS_ECHO_SERVER_URL"), &websocket.DialOptions{
				Subprotocols: []string{"echo"},
			})
			assert.Success(t, err)
			defer c.Close(websocket.StatusNormalClosure, "")
			nc := websocket.NetConn(ctx, c, websocket.MessageBinary)
			if buffered {
				_, err = nc.Write([]byte("hello"))
				assert.Success(t, err)
				first := make([]byte, 1)
				_, err = nc.Read(first)
				assert.Success(t, err)
				assert.Equal(t, "first byte", "h", string(first))
			}
			for _, empty := range [][]byte{nil, {}} {
				done := make(chan struct{})
				go func() {
					defer close(done)
					n, err := nc.Read(empty)
					assert.Success(t, err)
					assert.Equal(t, "read count", 0, n)
				}()
				select {
				case <-done:
				case <-time.After(time.Second):
					c.Close(websocket.StatusNormalClosure, "")
					<-done
					t.Fatal("zero-length Read blocked on an open connection")
				}
			}
			if buffered {
				rest := make([]byte, 4)
				_, err = nc.Read(rest)
				assert.Success(t, err)
				assert.Equal(t, "remaining message", "ello", string(rest))
			}
			assert.Success(t, c.Close(websocket.StatusNormalClosure, ""))
		})
	}
}
