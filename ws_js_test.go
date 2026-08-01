package websocket_test

import (
	"context"
	"net/http"
	"os"
	"syscall/js"
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

func TestWasmDialTimeout(t *testing.T) {
	js.Global().Call("eval", `(() => {
		globalThis.__originalWebSocket = globalThis.WebSocket;
		globalThis.__activeWebSocketListeners = 0;
		const trackedEvents = new Set(["close", "error", "message", "open"]);
		globalThis.WebSocket = class extends globalThis.__originalWebSocket {
			addEventListener(type, listener, options) {
				if (trackedEvents.has(type)) globalThis.__activeWebSocketListeners++;
				return super.addEventListener(type, listener, options);
			}
			removeEventListener(type, listener, options) {
				if (trackedEvents.has(type)) globalThis.__activeWebSocketListeners--;
				return super.removeEventListener(type, listener, options);
			}
		};
	})()`)
	defer js.Global().Call("eval", `(() => {
		globalThis.WebSocket = globalThis.__originalWebSocket;
		delete globalThis.__originalWebSocket;
		delete globalThis.__activeWebSocketListeners;
	})()`)

	beforeDial := time.Now()
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, _, err := websocket.Dial(ctx, "ws://example.com:9893", &websocket.DialOptions{
			Subprotocols: []string{"echo"},
		})
		assert.Error(t, err)
		assert.Equal(t, "active WebSocket event listeners", 0, js.Global().Get("__activeWebSocketListeners").Int())
	}
	if time.Since(beforeDial) >= time.Second {
		t.Fatal("wasm context dial timeout is not working", time.Since(beforeDial))
	}
}
