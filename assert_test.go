package websocket_test

import (
	"context"
	"math/rand"
	"strings"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/assert"
	"nhooyr.io/websocket/wsjson"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func assertJSONEcho(ctx context.Context, c *websocket.Conn, n int) error {
	exp := randString(n)
	err := wsjson.Write(ctx, c, exp)
	if err != nil {
		return err
	}

	var act interface{}
	err = wsjson.Read(ctx, c, &act)
	if err != nil {
		return err
	}

	return assert.Equalf(exp, act, "unexpected JSON")
}

func assertJSONRead(ctx context.Context, c *websocket.Conn, exp interface{}) error {
	var act interface{}
	err := wsjson.Read(ctx, c, &act)
	if err != nil {
		return err
	}

	return assert.Equalf(exp, act, "unexpected JSON")
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
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

func assertEcho(ctx context.Context, c *websocket.Conn, typ websocket.MessageType, n int) error {
	p := randBytes(n)
	err := c.Write(ctx, typ, p)
	if err != nil {
		return err
	}
	typ2, p2, err := c.Read(ctx)
	if err != nil {
		return err
	}
	err = assert.Equalf(typ, typ2, "unexpected data type")
	if err != nil {
		return err
	}
	return assert.Equalf(p, p2, "unexpected payload")
}

func assertSubprotocol(c *websocket.Conn, exp string) error {
	return assert.Equalf(exp, c.Subprotocol(), "unexpected subprotocol")
}
