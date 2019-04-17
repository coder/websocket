package websocket_test

import (
	"context"
	"io"
	"net/http"
	"nhooyr.io/websocket"
	"strings"
	"testing"
	"time"
)

func BenchmarkConn(b *testing.B) {
	b.StopTimer()

	s, closeFn := testServer(b, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r,
			websocket.AcceptSubprotocols("echo"),
		)
		if err != nil {
			b.Logf("server handshake failed: %+v", err)
			return
		}
		echoLoop(r.Context(), c)
	}))
	defer closeFn()

	wsURL := strings.Replace(s.URL, "http", "ws", 1)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL)
	if err != nil {
		b.Fatalf("failed to dial: %v", err)
	}
	defer c.Close(websocket.StatusInternalError, "")

	msg := strings.Repeat("2", 4096*16)
	buf := make([]byte, len(msg))
	b.SetBytes(int64(len(msg)))
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		w, err := c.Write(ctx, websocket.MessageText)
		if err != nil {
			b.Fatal(err)
		}

		_, err = io.WriteString(w, msg)
		if err != nil {
			b.Fatal(err)
		}

		err = w.Close()
		if err != nil {
			b.Fatal(err)
		}

		_, r, err := c.Read(ctx)
		if err != nil {
			b.Fatal(err, b.N)
		}

		_, err = io.ReadFull(r, buf)
		if err != nil {
			b.Fatal(err)
		}

		// TODO jank
		_, err = r.Read(nil)
		if err != io.EOF {
			b.Fatalf("wtf %q", err)
		}
	}
	b.StopTimer()
	c.Close(websocket.StatusNormalClosure, "")
}
