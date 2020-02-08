package websocket_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"cdr.dev/slog"
	"cdr.dev/slog/sloggers/slogtest"
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

func echoJSON(t *testing.T, c *websocket.Conn, n int) {
	slog.Helper()

	s := randString(t, n)
	writeJSON(t, c, s)
	readJSON(t, c, s)
}

func writeJSON(t *testing.T, c *websocket.Conn, v interface{}) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	err := wsjson.Write(ctx, c, v)
	assert.Success(t, "wsjson.Write", err)
}

func readJSON(t *testing.T, c *websocket.Conn, exp interface{}) {
	slog.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	var act interface{}
	err := wsjson.Read(ctx, c, &act)
	assert.Success(t, "wsjson.Read", err)
	assert.Equal(t, "json", exp, act)
}

func randString(t *testing.T, n int) string {
	s := strings.ToValidUTF8(string(randBytes(t, n)), "_")
	s = strings.ReplaceAll(s, "\x00", "_")
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
	slog.Helper()

	p := randBytes(t, n)
	err := c.Write(ctx, typ, p)
	assert.Success(t, "write", err)

	typ2, p2, err := c.Read(ctx)
	assert.Success(t, "read", err)

	assert.Equal(t, "dataType", typ, typ2)
	assert.Equal(t, "payload", p, p2)
}

func assertSubprotocol(t *testing.T, c *websocket.Conn, exp string) {
	slog.Helper()

	assert.Equal(t, "subprotocol", exp, c.Subprotocol())
}

func assertCloseStatus(t testing.TB, exp websocket.StatusCode, err error) {
	slog.Helper()

	if websocket.CloseStatus(err) == -1 {
		slogtest.Fatal(t, "expected websocket.CloseError", slogType(err), slog.Error(err))
	}
	if websocket.CloseStatus(err) != exp {
		slogtest.Error(t, "unexpected close status",
			slog.F("exp", exp),
			slog.F("act", err),
		)
	}

}

func acceptWebSocket(t testing.TB, r *http.Request, w http.ResponseWriter, opts *websocket.AcceptOptions) *websocket.Conn {
	c, err := websocket.Accept(w, r, opts)
	assert.Success(t, "websocket.Accept", err)
	return c
}

func slogType(v interface{}) slog.Field {
	return slog.F("type", fmt.Sprintf("%T", v))
}
