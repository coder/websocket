package websocket_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"nhooyr.io/websocket"
)

var httpclient = &http.Client{
	Timeout: time.Second * 15,
}

func TestHandshake(t *testing.T) {
	t.Parallel()

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r,
			websocket.AcceptSubprotocols("myproto"),
		)
		if err != nil {
			t.Errorf("failed to accept connection: %v", err)
			return
		}
		_ = c
	}))
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, s.URL,
		websocket.DialSubprotocols("myproto"),
	)
	if err != nil {
		t.Fatalf("failed to do handshake request: %v", err)
	}
	_ = c

	checkHeader := func(h, exp string) {
		t.Helper()
		value := resp.Header.Get(h)
		if exp != value {
			t.Errorf("expected different value for header %v: %v", h, cmp.Diff(exp, value))
		}
	}

	checkHeader("Connection", "Upgrade")
	checkHeader("Upgrade", "websocket")
	checkHeader("Sec-WebSocket-Accept", "ICX+Yqv66kxgM0FcWaLWlFLwTAI=")
	checkHeader("Sec-WebSocket-Protocol", "myproto")
}
