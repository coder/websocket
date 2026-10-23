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
	assert.Equal(t, "close error", "failed to close WebSocket: websocket: close reason truncated from 124 to 122 bytes", err.Error())
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
