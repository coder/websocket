package websocket_test

import (
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func BenchmarkConn(b *testing.B) {
	sizes := []int{
		2,
		16,
		32,
		512,
		4096,
		16384,
	}

	b.Run("write", func(b *testing.B) {
		for _, size := range sizes {
			b.Run(strconv.Itoa(size), func(b *testing.B) {
				b.Run("stream", func(b *testing.B) {
					benchConn(b, false, true, size)
				})
				b.Run("buffer", func(b *testing.B) {
					benchConn(b, false, false, size)
				})
			})
		}
	})

	b.Run("echo", func(b *testing.B) {
		for _, size := range sizes {
			b.Run(strconv.Itoa(size), func(b *testing.B) {
				benchConn(b, true, true, size)
			})
		}
	})
}

func benchConn(b *testing.B, echo, stream bool, size int) {
	s, closeFn := testServer(b, func(w http.ResponseWriter, r *http.Request) error {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return err
		}
		if echo {
			echoLoop(r.Context(), c)
		} else {
			discardLoop(r.Context(), c)
		}
		return nil
	}, false)
	defer closeFn()

	wsURL := strings.Replace(s.URL, "http", "ws", 1)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close(websocket.StatusInternalError, "")

	msg := []byte(strings.Repeat("2", size))
	readBuf := make([]byte, len(msg))
	b.SetBytes(int64(len(msg)))
	b.ReportAllocs()
	b.ResetTimer()
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

		if echo {
			_, r, err := c.Reader(ctx)
			if err != nil {
				b.Fatal(err)
			}

			_, err = io.ReadFull(r, readBuf)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
	b.StopTimer()

	c.Close(websocket.StatusNormalClosure, "")
}

func discardLoop(ctx context.Context, c *websocket.Conn) {
	defer c.Close(websocket.StatusInternalError, "")

	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	b := make([]byte, 32768)
	echo := func() error {
		_, r, err := c.Reader(ctx)
		if err != nil {
			return err
		}

		_, err = io.CopyBuffer(ioutil.Discard, r, b)
		if err != nil {
			return err
		}
		return nil
	}

	for {
		err := echo()
		if err != nil {
			return
		}
	}
}
