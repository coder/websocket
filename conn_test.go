// +build !js

package websocket_test

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"
	"golang.org/x/xerrors"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/test/cmp"
	"nhooyr.io/websocket/internal/test/wstest"
	"nhooyr.io/websocket/internal/test/xrand"
	"nhooyr.io/websocket/internal/xsync"
	"nhooyr.io/websocket/wsjson"
	"nhooyr.io/websocket/wspb"
)

func TestConn(t *testing.T) {
	t.Parallel()

	t.Run("fuzzData", func(t *testing.T) {
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

				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
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

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
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

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
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

	t.Run("concurrentWriteError", func(t *testing.T) {
		t.Parallel()

		c1, c2, err := wstest.Pipe(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c2.Close(websocket.StatusInternalError, "")
		defer c1.Close(websocket.StatusInternalError, "")

		_, err = c1.Writer(context.Background(), websocket.MessageText)
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)
		defer cancel()

		err = c1.Write(ctx, websocket.MessageText, []byte("x"))
		if !xerrors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	})

	t.Run("netConn", func(t *testing.T) {
		t.Parallel()

		c1, c2, err := wstest.Pipe(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c2.Close(websocket.StatusInternalError, "")
		defer c1.Close(websocket.StatusInternalError, "")

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
		defer cancel()

		n1 := websocket.NetConn(ctx, c1, websocket.MessageBinary)
		n2 := websocket.NetConn(ctx, c2, websocket.MessageBinary)

		// Does not give any confidence but at least ensures no crashes.
		d, _ := ctx.Deadline()
		n1.SetDeadline(d)
		n1.SetDeadline(time.Time{})

		if n1.RemoteAddr() != n1.LocalAddr() {
			t.Fatal()
		}
		if n1.RemoteAddr().String() != "websocket/unknown-addr" || n1.RemoteAddr().Network() != "websocket" {
			t.Fatal(n1.RemoteAddr())
		}

		errs := xsync.Go(func() error {
			_, err := n2.Write([]byte("hello"))
			if err != nil {
				return err
			}
			return n2.Close()
		})

		b, err := ioutil.ReadAll(n1)
		if err != nil {
			t.Fatal(err)
		}

		_, err = n1.Read(nil)
		if err != io.EOF {
			t.Fatalf("expected EOF: %v", err)
		}

		err = <-errs
		if err != nil {
			t.Fatal(err)
		}

		if !cmp.Equal([]byte("hello"), b) {
			t.Fatalf("unexpected msg: %v", cmp.Diff([]byte("hello"), b))
		}
	})

	t.Run("netConn/BadMsg", func(t *testing.T) {
		t.Parallel()

		c1, c2, err := wstest.Pipe(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c2.Close(websocket.StatusInternalError, "")
		defer c1.Close(websocket.StatusInternalError, "")

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
		defer cancel()

		n1 := websocket.NetConn(ctx, c1, websocket.MessageBinary)
		n2 := websocket.NetConn(ctx, c2, websocket.MessageText)

		errs := xsync.Go(func() error {
			_, err := n2.Write([]byte("hello"))
			if err != nil {
				return err
			}
			return nil
		})

		_, err = ioutil.ReadAll(n1)
		if !cmp.ErrorContains(err, `unexpected frame type read (expected MessageBinary): MessageText`) {
			t.Fatal(err)
		}

		err = <-errs
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("wsjson", func(t *testing.T) {
		t.Parallel()

		c1, c2, err := wstest.Pipe(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c2.Close(websocket.StatusInternalError, "")
		defer c1.Close(websocket.StatusInternalError, "")

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
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

		c1.SetReadLimit(1 << 30)

		exp := xrand.String(xrand.Int(131072))

		werr := xsync.Go(func() error {
			return wsjson.Write(ctx, c1, exp)
		})

		var act interface{}
		err = wsjson.Read(ctx, c1, &act)
		if err != nil {
			t.Fatal(err)
		}
		if exp != act {
			t.Fatal(cmp.Diff(exp, act))
		}

		err = <-werr
		if err != nil {
			t.Fatal(err)
		}

		err = c1.Close(websocket.StatusNormalClosure, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("wspb", func(t *testing.T) {
		t.Parallel()

		c1, c2, err := wstest.Pipe(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c2.Close(websocket.StatusInternalError, "")
		defer c1.Close(websocket.StatusInternalError, "")

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
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

		exp := ptypes.DurationProto(100)
		err = wspb.Write(ctx, c1, exp)
		if err != nil {
			t.Fatal(err)
		}

		act := &duration.Duration{}
		err = wspb.Read(ctx, c1, act)
		if err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(exp, act) {
			t.Fatal(cmp.Diff(exp, act))
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

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
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
