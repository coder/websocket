package websocket_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/assert"
)

func TestConn(t *testing.T) {
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

	err = assertSubprotocol(c, "echo")
	if err != nil {
		t.Fatal(err)
	}

	err = assert.Equalf(&http.Response{}, resp, "unexpected http response")
	if err != nil {
		t.Fatal(err)
	}

	err = assertJSONEcho(ctx, c, 1024)
	if err != nil {
		t.Fatal(err)
	}

	err = assertEcho(ctx, c, websocket.MessageBinary, 1024)
	if err != nil {
		t.Fatal(err)
	}

	err = c.Close(websocket.StatusNormalClosure, "")
	if err != nil {
		t.Fatal(err)
	}
}
