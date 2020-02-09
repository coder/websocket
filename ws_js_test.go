package websocket_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/test/cmp"
	"nhooyr.io/websocket/internal/test/wstest"
)

func TestWasm(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	c, resp, err := websocket.Dial(ctx, os.Getenv("WS_ECHO_SERVER_URL"), &websocket.DialOptions{
		Subprotocols: []string{"echo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(websocket.StatusInternalError, "")

	if !cmp.Equal("echo", c.Subprotocol()) {
		t.Fatalf("unexpected subprotocol: %v", cmp.Diff("echo", c.Subprotocol()))
	}
	if !cmp.Equal(http.StatusSwitchingProtocols, resp.StatusCode) {
		t.Fatalf("unexpected status code: %v", cmp.Diff(http.StatusSwitchingProtocols, resp.StatusCode))
	}

	c.SetReadLimit(65536)
	for i := 0; i < 10; i++ {
		err = wstest.Echo(ctx, c, 65536)
		if err != nil {
			t.Fatal(err)
		}
	}

	err = c.Close(websocket.StatusNormalClosure, "")
	if err != nil {
		t.Fatal(err)
	}
}
