package websocket_test

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"

	"cdr.dev/slog"
	"cdr.dev/slog/sloggers/slogtest/assert"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

func randBytes(t *testing.T, n int) []byte {
	b := make([]byte, n)
	_, err := rand.Reader.Read(b)
	assert.Success(t, "readRandBytes", err)
	return b
}

func assertJSONEcho(t *testing.T, ctx context.Context, c *websocket.Conn, n int) {
	t.Helper()
	defer c.Close(websocket.StatusInternalError, "")

	exp := randString(t, n)
	err := wsjson.Write(ctx, c, exp)
	assert.Success(t, "wsjson.Write", err)

	assertJSONRead(t, ctx, c, exp)

	c.Close(websocket.StatusNormalClosure, "")
}

func assertJSONRead(t *testing.T, ctx context.Context, c *websocket.Conn, exp interface{}) {
	slog.Helper()

	var act interface{}
	err := wsjson.Read(ctx, c, &act)
	assert.Success(t, "wsjson.Read", err)

	assert.Equal(t, "json", exp, act)
}

func randString(t *testing.T, n int) string {
	s := strings.ToValidUTF8(string(randBytes(t, n)), "_")
	if len(s) > n {
		return s[:n]
	}
	if len(s) < n {
		// Pad with =
		extra := n - len(s)
		return s + strings.Repeat("=", extra)
	}
	return s
}

func assertEcho(t *testing.T, ctx context.Context, c *websocket.Conn, typ websocket.MessageType, n int) {
	t.Helper()

	p := randBytes(t, n)
	err := c.Write(ctx, typ, p)
	assert.Success(t, "write", err)

	typ2, p2, err := c.Read(ctx)
	assert.Success(t, "read", err)

	assert.Equal(t, "dataType", typ, typ2)
	assert.Equal(t, "payload", p, p2)
}

func assertSubprotocol(t *testing.T, c *websocket.Conn, exp string) {
	t.Helper()

	assert.Equal(t, "subprotocol", exp, c.Subprotocol())
}

func assertCloseStatus(t *testing.T, exp websocket.StatusCode, err error) {
	t.Helper()
	defer func() {
		if t.Failed() {
			t.Logf("error: %+v", err)
		}
	}()
	assert.Equal(t, "closeStatus", exp, websocket.CloseStatus(err))
}
