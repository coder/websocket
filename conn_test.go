// +build !js

package websocket_test

import (
	"context"
	"io"
	"testing"
	"time"

	"golang.org/x/xerrors"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/test/cmp"
	"nhooyr.io/websocket/internal/test/wstest"
	"nhooyr.io/websocket/internal/test/xrand"
	"nhooyr.io/websocket/wsjson"
)

func goFn(fn func() error) chan error {
	errs := make(chan error)
	go func() {
		defer close(errs)
		errs <- fn()
	}()

	return errs
}

func TestConn(t *testing.T) {
	t.Parallel()

	t.Run("data", func(t *testing.T) {
		t.Parallel()

		for i := 0; i < 10; i++ {
			t.Run("", func(t *testing.T) {
				t.Parallel()

				ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
				defer cancel()

				copts := websocket.CompressionOptions{
					Mode:      websocket.CompressionMode(xrand.Int(int(websocket.CompressionDisabled))),
					Threshold: xrand.Int(9999),
				}

				c1, c2, err := wstest.Pipe(&websocket.DialOptions{
					CompressionOptions: copts,
				}, &websocket.AcceptOptions{
					CompressionOptions: copts,
				})
				if err != nil {
					t.Fatal(err)
				}
				defer c1.Close(websocket.StatusInternalError, "")
				defer c2.Close(websocket.StatusInternalError, "")

				echoLoopErr := goFn(func() error {
					err := echoLoop(ctx, c1)
					return assertCloseStatus(websocket.StatusNormalClosure, err)
				})
				defer func() {
					err := <-echoLoopErr
					if err != nil {
						t.Errorf("echo loop error: %v", err)
					}
				}()
				defer cancel()

				c2.SetReadLimit(1 << 30)

				for i := 0; i < 10; i++ {
					n := xrand.Int(131_072)

					msg := xrand.String(n)

					writeErr := goFn(func() error {
						return wsjson.Write(ctx, c2, msg)
					})

					var act interface{}
					err := wsjson.Read(ctx, c2, &act)
					if err != nil {
						t.Fatal(err)
					}

					err = <-writeErr
					if err != nil {
						t.Fatal(err)
					}

					if !cmp.Equal(msg, act) {
						t.Fatalf("unexpected msg read: %v", cmp.Diff(msg, act))
					}
				}

				c2.Close(websocket.StatusNormalClosure, "")
			})
		}
	})
}

func assertCloseStatus(exp websocket.StatusCode, err error) error {
	if websocket.CloseStatus(err) == -1 {
		return xerrors.Errorf("expected websocket.CloseError: %T %v", err, err)
	}
	if websocket.CloseStatus(err) != exp {
		return xerrors.Errorf("unexpected close status (%v):%v", exp, err)
	}
	return nil
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
