package websocket_test

import (
	"context"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func TestWebSocket(t *testing.T) {
	t.Parallel()

	_, _, err := websocket.Dial(context.Background(), "ws://localhost:8081", nil)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Second)
}
