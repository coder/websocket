// +build !js

package websocket_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cdr.dev/slog/sloggers/slogtest/assert"

	"nhooyr.io/websocket"
)

func goFn(fn func()) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()

	return func() {
		<-done
	}
}

func TestConn(t *testing.T) {
	t.Parallel()

	t.Run("json", func(t *testing.T) {
		t.Parallel()

		for i := 0; i < 1; i++ {
			t.Run("", func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()

				c1, c2 := websocketPipe(t)

				wait := goFn(func() {
					err := echoLoop(ctx, c1)
					assertCloseStatus(t, websocket.StatusNormalClosure, err)
				})
				defer wait()

				c2.SetReadLimit(1 << 30)

				for i := 0; i < 10; i++ {
					n := randInt(t, 131_072)
					echoJSON(t, c2, n)
				}

				c2.Close(websocket.StatusNormalClosure, "")
			})
		}
	})
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}

// echoLoop echos every msg received from c until an error
// occurs or the context expires.
// The read limit is set to 1 << 30.
func echoLoop(ctx context.Context, c *websocket.Conn) error {
	defer c.Close(websocket.StatusInternalError, "")

	c.SetReadLimit(1 << 30)

	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	b := make([]byte, 32<<10)
	for {
		typ, r, err := c.Reader(ctx)
		if err != nil {
			return err
		}

		w, err := c.Writer(ctx, typ)
		if err != nil {
			return err
		}

		_, err = io.CopyBuffer(w, r, b)
		if err != nil {
			return err
		}

		err = w.Close()
		if err != nil {
			return err
		}
	}
}

func randBool(t testing.TB) bool  {
	return randInt(t, 2) == 1
}

func randInt(t testing.TB, max int) int {
	x, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	assert.Success(t, "rand.Int", err)
	return int(x.Int64())
}

type testHijacker struct {
	*httptest.ResponseRecorder
	serverConn net.Conn
	hijacked chan struct{}
}

var _ http.Hijacker = testHijacker{}

func (hj testHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	close(hj.hijacked)
	return hj.serverConn, bufio.NewReadWriter(bufio.NewReader(hj.serverConn), bufio.NewWriter(hj.serverConn)), nil
}

func websocketPipe(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	var serverConn *websocket.Conn
	tt := testTransport{
		h: func(w http.ResponseWriter, r *http.Request) {
			serverConn = acceptWebSocket(t, r, w, nil)
		},
	}

	dialOpts := &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: tt,
		},
	}

	clientConn, _, err := websocket.Dial(context.Background(), "ws://example.com", dialOpts)
	assert.Success(t, "websocket.Dial", err)

	if randBool(t) {
		return serverConn, clientConn
	}
	return clientConn, serverConn
}

type testTransport struct {
	h http.HandlerFunc
}

func (t testTransport) RoundTrip(r *http.Request) (*http.Response, error)  {
	clientConn, serverConn := net.Pipe()

	hj := testHijacker{
		ResponseRecorder: httptest.NewRecorder(),
		serverConn:       serverConn,
		hijacked:         make(chan struct{}),
	}

	done := make(chan struct{})
	t.h.ServeHTTP(hj, r)

	select {
	case <-hj.hijacked:
		resp := hj.ResponseRecorder.Result()
		resp.Body = clientConn
		return resp, nil
	case <-done:
		return hj.ResponseRecorder.Result(), nil
	}
}
