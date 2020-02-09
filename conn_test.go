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
)

func goFn(fn func() error) chan error {
	errs := make(chan error)
	go func() {
		defer func() {
			r := recover()
			if r != nil {
				errs <- xerrors.Errorf("panic in gofn: %v", r)
			}
		}()
		errs <- fn()
	}()

	return errs
}

func TestConn(t *testing.T) {
	t.Parallel()

	t.Run("data", func(t *testing.T) {
		t.Parallel()

		for i := 0; i < 5; i++ {
			t.Run("", func(t *testing.T) {
				t.Parallel()

				ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
				defer cancel()

				copts := websocket.CompressionOptions{
					Mode:      websocket.CompressionMode(xrand.Int(int(websocket.CompressionDisabled) + 1)),
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

				for i := 0; i < 5; i++ {
					n := xrand.Int(131_072)

					msg := xrand.Bytes(n)

					expType := websocket.MessageBinary
					if xrand.Bool() {
						expType = websocket.MessageText
					}

					writeErr := goFn(func() error {
						return c2.Write(ctx, expType, msg)
					})

					actType, act, err := c2.Read(ctx)
					if err != nil {
						t.Fatal(err)
					}

					err = <-writeErr
					if err != nil {
						t.Fatal(err)
					}

					if expType != actType {
						t.Fatalf("unexpected message typ (%v): %v", expType, actType)
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
