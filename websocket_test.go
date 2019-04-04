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

func TestConnection(t *testing.T) {
	t.Parallel()

	obj := make(chan interface{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r,
			websocket.AcceptSubprotocols("myproto"),
		)
		if err != nil {
			t.Errorf("failed to accept connection: %v", err)
			return
		}
		defer c.Close(websocket.StatusInternalError, "")

		ctx, cancel := context.WithTimeout(r.Context(), time.Second*5)
		defer cancel()

		var v interface{}
		err = websocket.ReadJSON(ctx, c, &v)
		if err != nil {
			t.Error(err)
			return
		}

		t.Log("success", v)
		obj <- v

		c.Close(websocket.StatusNormalClosure, "")
	}))
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, s.URL,
		websocket.DialSubprotocols("myproto"),
	)
	if err != nil {
		t.Fatalf("failed to do handshake request: %v", err)
	}
	defer c.Close(websocket.StatusInternalError, "")

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

	v := map[string]interface{}{
		"anmol": "wowow",
	}
	err = websocket.WriteJSON(ctx, c, v)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case v2 := <-obj:
		if !cmp.Equal(v, v2) {
			t.Fatalf("unexpected value read: %v", cmp.Diff(v, v2))
		}
	case <-time.After(time.Second * 10):
		t.Fatalf("test timed out")
	}
}
