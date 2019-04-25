package websocket_test

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func benchConn(b *testing.B, stream bool) {
	name := "buffered"
	if stream {
		name = "stream"
	}

	b.Run(name, func(b *testing.B) {
		s, closeFn := testServer(b, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := websocket.Accept(w, r, websocket.AcceptOptions{})
			if err != nil {
				b.Logf("server handshake failed: %+v", err)
				return
			}
			if stream {
				streamEchoLoop(r.Context(), c)
			} else {
				bufferedEchoLoop(r.Context(), c)
			}

		}))
		defer closeFn()

		wsURL := strings.Replace(s.URL, "http", "ws", 1)

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
		defer cancel()

		c, _, err := websocket.Dial(ctx, wsURL, websocket.DialOptions{})
		if err != nil {
			b.Fatalf("failed to dial: %v", err)
		}
		defer c.Close(websocket.StatusInternalError, "")

		runN := func(n int) {
			msg := []byte(strings.Repeat("2", n))
			buf := make([]byte, len(msg))
			b.Run(strconv.Itoa(n), func(b *testing.B) {
				b.SetBytes(int64(len(msg)))
				for i := 0; i < b.N; i++ {
					if stream {
						w, err := c.Writer(ctx, websocket.MessageText)
						if err != nil {
							b.Fatal(err)
						}

						_, err = w.Write(msg)
						if err != nil {
							b.Fatal(err)
						}

						err = w.Close()
						if err != nil {
							b.Fatal(err)
						}
					} else {
						err = c.Write(ctx, websocket.MessageText, msg)
						if err != nil {
							b.Fatal(err)
						}
					}
					_, r, err := c.Reader(ctx)
					if err != nil {
						b.Fatal(err, b.N)
					}

					_, err = io.ReadFull(r, buf)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}

		runN(32)
		runN(128)
		runN(512)
		runN(1024)
		runN(4096)
		runN(16384)
		runN(65536)
		runN(131072)

		c.Close(websocket.StatusNormalClosure, "")
	})
}

func BenchmarkConn(b *testing.B) {
	benchConn(b, true)
	benchConn(b, false)
}
