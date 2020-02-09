package websocket

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestEcho(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	c, resp, err := Dial(ctx, os.Getenv("WS_ECHO_SERVER_URL"), &DialOptions{
		Subprotocols: []string{"echo"},
	})
	assert.Success(t, err)
	defer c.Close(StatusInternalError, "")

	assertSubprotocol(t, c, "echo")
	assert.Equalf(t, &http.Response{}, resp, "http.Response")
	echoJSON(t, ctx, c, 1024)
	assertEcho(t, ctx, c, MessageBinary, 1024)

	err = c.Close(StatusNormalClosure, "")
	assert.Success(t, err)
}
