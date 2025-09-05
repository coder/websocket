package websocket_test

import (
	"context"
	"os"
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

	// NOTE: the response from websocket.Dial is a mock on js and not actually used.
	c, _, err := websocket.Dial(ctx, os.Getenv("WS_ECHO_SERVER_URL"), &websocket.DialOptions{
		Subprotocols: []string{"echo"},
	})
	assert.Success(t, err)
	defer c.Close(websocket.StatusInternalError, "")

	assert.Equal(t, "subprotocol", "echo", c.Subprotocol())

	c.SetReadLimit(65536)
	for range 10 {
		err = wstest.Echo(ctx, c, 65536)
		assert.Success(t, err)
	}

	err = c.Close(websocket.StatusNormalClosure, "")
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
