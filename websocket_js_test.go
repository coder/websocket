package websocket_test

import (
	"context"
	"flag"
	"net/http"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func TestWebSocket(t *testing.T) {
	t.Parallel()

	wsEchoServerURL := flag.Arg(0)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	c, resp, err := websocket.Dial(ctx, wsEchoServerURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(websocket.StatusInternalError, "")

	err = assertEqualf(&http.Response{}, resp, "unexpected http response")
	if err != nil {
		t.Fatal(err)
	}

	err = assertJSONEcho(ctx, c, 4096)
	if err != nil {
		t.Fatal(err)
	}

	err = c.Close(websocket.StatusNormalClosure, "")
	if err != nil {
		t.Fatal(err)
	}
}
