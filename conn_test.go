// +build !js

package websocket_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"golang.org/x/xerrors"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/test/cmp"
	"nhooyr.io/websocket/internal/test/wstest"
	"nhooyr.io/websocket/internal/test/xrand"
	"nhooyr.io/websocket/internal/xsync"
)

func TestConn(t *testing.T) {
	t.Parallel()

	t.Run("data", func(t *testing.T) {
		t.Parallel()

		for i := 0; i < 5; i++ {
			t.Run("", func(t *testing.T) {
				t.Parallel()

				copts := &websocket.CompressionOptions{
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
				defer c2.Close(websocket.StatusInternalError, "")
				defer c1.Close(websocket.StatusInternalError, "")

				ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
				defer cancel()

				echoLoopErr := xsync.Go(func() error {
					err := wstest.EchoLoop(ctx, c2)
					return assertCloseStatus(websocket.StatusNormalClosure, err)
				})
				defer func() {
					err := <-echoLoopErr
					if err != nil {
						t.Errorf("echo loop error: %v", err)
					}
				}()
				defer cancel()

				c1.SetReadLimit(131072)

				for i := 0; i < 5; i++ {
					err := wstest.Echo(ctx, c1, 131072)
					if err != nil {
						t.Fatal(err)
					}
				}

				err = c1.Close(websocket.StatusNormalClosure, "")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			})
		}
	})

	t.Run("badClose", func(t *testing.T) {
		t.Parallel()

		c1, c2, err := wstest.Pipe(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c1.Close(websocket.StatusInternalError, "")
		defer c2.Close(websocket.StatusInternalError, "")

		err = c1.Close(-1, "")
		if !cmp.ErrorContains(err, "failed to marshal close frame: status code StatusCode(-1) cannot be set") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ping", func(t *testing.T) {
		t.Parallel()

		c1, c2, err := wstest.Pipe(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c1.Close(websocket.StatusInternalError, "")
		defer c2.Close(websocket.StatusInternalError, "")

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
		defer cancel()

		c2.CloseRead(ctx)
		c1.CloseRead(ctx)

		for i := 0; i < 10; i++ {
			err = c1.Ping(ctx)
			if err != nil {
				t.Fatal(err)
			}
		}

		err = c1.Close(websocket.StatusNormalClosure, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("badPing", func(t *testing.T) {
		t.Parallel()

		c1, c2, err := wstest.Pipe(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c1.Close(websocket.StatusInternalError, "")
		defer c2.Close(websocket.StatusInternalError, "")

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()

		c2.CloseRead(ctx)

		err = c1.Ping(ctx)
		if !cmp.ErrorContains(err, "failed to wait for pong") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("concurrentWrite", func(t *testing.T) {
		t.Parallel()

		c1, c2, err := wstest.Pipe(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c2.Close(websocket.StatusInternalError, "")
		defer c1.Close(websocket.StatusInternalError, "")

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()

		discardLoopErr := xsync.Go(func() error {
			for {
				_, _, err := c2.Read(ctx)
				if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
					return nil
				}
				if err != nil {
					return err
				}
			}
		})
		defer func() {
			err := <-discardLoopErr
			if err != nil {
				t.Errorf("discard loop error: %v", err)
			}
		}()
		defer cancel()

		msg := xrand.Bytes(xrand.Int(9999))
		const count = 100
		errs := make(chan error, count)

		for i := 0; i < count; i++ {
			go func() {
				errs <- c1.Write(ctx, websocket.MessageBinary, msg)
			}()
		}

		for i := 0; i < count; i++ {
			err := <-errs
			if err != nil {
				t.Fatal(err)
			}
		}

		err = c1.Close(websocket.StatusNormalClosure, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestWasm(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wg.Add(1)
		defer wg.Done()

		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols:       []string{"echo"},
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close(websocket.StatusInternalError, "")

		err = wstest.EchoLoop(r.Context(), c)
		if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
			t.Errorf("echoLoop: %v", err)
		}
	}))
	defer wg.Wait()
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*20)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "test", "-exec=wasmbrowsertest", "./...")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", fmt.Sprintf("WS_ECHO_SERVER_URL=%v", wstest.URL(s)))

	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wasm test binary failed: %v:\n%s", err, b)
	}
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
