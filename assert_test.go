package websocket_test

import (
	"context"
	"math/rand"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/assert"
	"nhooyr.io/websocket/wsjson"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func assertJSONEcho(t *testing.T, ctx context.Context, c *websocket.Conn, n int) {
	exp := randString(n)
	err := wsjson.Write(ctx, c, exp)
	assert.Success(t, err)

	var act interface{}
	err = wsjson.Read(ctx, c, &act)
	assert.Success(t, err)

	assert.Equalf(t, exp, act, "unexpected JSON")
}

func assertJSONRead(t *testing.T, ctx context.Context, c *websocket.Conn, exp interface{}) {
	var act interface{}
	err := wsjson.Read(ctx, c, &act)
	assert.Success(t, err)

	assert.Equalf(t, exp, act, "unexpected JSON")
}

func randString(n int) string {
	s := strings.ToValidUTF8(string(randBytes(n)), "_")
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
	p := randBytes(n)
	err := c.Write(ctx, typ, p)
	assert.Success(t, err)

	typ2, p2, err := c.Read(ctx)
	assert.Success(t, err)

	assert.Equalf(t, typ, typ2, "unexpected data type")
	assert.Equalf(t, p, p2, "unexpected payload")
}

func assertSubprotocol(t *testing.T, c *websocket.Conn, exp string) {
	assert.Equalf(t, exp, c.Subprotocol(), "unexpected subprotocol")
}
