package websocket_test

import (
	"context"
	"crypto/rand"
	"io"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/assert"
	"nhooyr.io/websocket/wsjson"
	"strings"
	"testing"
)

func randBytes(t *testing.T, n int) []byte {
	b := make([]byte, n)
	_, err := io.ReadFull(rand.Reader, b)
	assert.Success(t, err)
	return b
}

func assertJSONEcho(t *testing.T, ctx context.Context, c *websocket.Conn, n int) {
	t.Helper()
	defer c.Close(websocket.StatusInternalError, "")

	exp := randString(t, n)
	err := wsjson.Write(ctx, c, exp)
	assert.Success(t, err)

	assertJSONRead(t, ctx, c, exp)

	c.Close(websocket.StatusNormalClosure, "")
}

func assertJSONRead(t *testing.T, ctx context.Context, c *websocket.Conn, exp interface{}) {
	t.Helper()

	var act interface{}
	err := wsjson.Read(ctx, c, &act)
	assert.Success(t, err)

	assert.Equal(t, exp, act, "JSON")
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
	assert.Success(t, err)

	typ2, p2, err := c.Read(ctx)
	assert.Success(t, err)

	assert.Equal(t, typ, typ2, "data type")
	assert.Equal(t, p, p2, "payload")
}

func assertSubprotocol(t *testing.T, c *websocket.Conn, exp string) {
	t.Helper()

	assert.Equal(t, exp, c.Subprotocol(), "subprotocol")
}

func assertCloseStatus(t *testing.T, exp websocket.StatusCode, err error) {
	t.Helper()
	defer func() {
		if t.Failed() {
			t.Logf("error: %+v", err)
		}
	}()
	assert.Equal(t, exp, websocket.CloseStatus(err), "StatusCode")
}
