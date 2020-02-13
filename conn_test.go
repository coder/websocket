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
				tt := newTest(t)
				defer tt.done()

				dialCopts := &websocket.CompressionOptions{
					Mode:      websocket.CompressionMode(xrand.Int(int(websocket.CompressionDisabled) + 1)),
					Threshold: xrand.Int(9999),
				}

				acceptCopts := &websocket.CompressionOptions{
					Mode:      websocket.CompressionMode(xrand.Int(int(websocket.CompressionDisabled) + 1)),
					Threshold: xrand.Int(9999),
				}

				c1, c2 := tt.pipe(&websocket.DialOptions{
					CompressionOptions: dialCopts,
				}, &websocket.AcceptOptions{
					CompressionOptions: acceptCopts,
				})

				tt.goEchoLoop(c2)

				c1.SetReadLimit(131072)

				for i := 0; i < 5; i++ {
					err := wstest.Echo(tt.ctx, c1, 131072)
					tt.success(err)
				}

				err := c1.Close(websocket.StatusNormalClosure, "")
				tt.success(err)
			})
		}
	})

	t.Run("badClose", func(t *testing.T) {
		tt := newTest(t)
		defer tt.done()

		c1, _ := tt.pipe(nil, nil)

		err := c1.Close(-1, "")
		tt.errContains(err, "failed to marshal close frame: status code StatusCode(-1) cannot be set")
	})

	t.Run("ping", func(t *testing.T) {
		tt := newTest(t)
		defer tt.done()

		c1, c2 := tt.pipe(nil, nil)

		c1.CloseRead(tt.ctx)
		c2.CloseRead(tt.ctx)

		for i := 0; i < 10; i++ {
			err := c1.Ping(tt.ctx)
			tt.success(err)
		}

		err := c1.Close(websocket.StatusNormalClosure, "")
		tt.success(err)
	})

	t.Run("badPing", func(t *testing.T) {
		tt := newTest(t)
		defer tt.done()

		c1, c2 := tt.pipe(nil, nil)

		c2.CloseRead(tt.ctx)

		err := c1.Ping(tt.ctx)
		tt.errContains(err, "failed to wait for pong")
	})

	t.Run("concurrentWrite", func(t *testing.T) {
		tt := newTest(t)
		defer tt.done()

		c1, c2 := tt.pipe(nil, nil)
		tt.goDiscardLoop(c2)

		msg := xrand.Bytes(xrand.Int(9999))
		const count = 100
		errs := make(chan error, count)

		for i := 0; i < count; i++ {
			go func() {
				errs <- c1.Write(tt.ctx, websocket.MessageBinary, msg)
			}()
		}

		for i := 0; i < count; i++ {
			err := <-errs
			tt.success(err)
		}

		err := c1.Close(websocket.StatusNormalClosure, "")
		tt.success(err)
	})

	t.Run("concurrentWriteError", func(t *testing.T) {
		tt := newTest(t)
		defer tt.done()

		c1, _ := tt.pipe(nil, nil)

		_, err := c1.Writer(tt.ctx, websocket.MessageText)
		tt.success(err)

		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)
		defer cancel()

		err = c1.Write(ctx, websocket.MessageText, []byte("x"))
		tt.eq(context.DeadlineExceeded, err)
	})

	t.Run("netConn", func(t *testing.T) {
		tt := newTest(t)
		defer tt.done()

		c1, c2 := tt.pipe(nil, nil)

		n1 := websocket.NetConn(tt.ctx, c1, websocket.MessageBinary)
		n2 := websocket.NetConn(tt.ctx, c2, websocket.MessageBinary)

		// Does not give any confidence but at least ensures no crashes.
		d, _ := tt.ctx.Deadline()
		n1.SetDeadline(d)
		n1.SetDeadline(time.Time{})

		tt.eq(n1.RemoteAddr(), n1.LocalAddr())
		tt.eq("websocket/unknown-addr", n1.RemoteAddr().String())
		tt.eq("websocket", n1.RemoteAddr().Network())

		errs := xsync.Go(func() error {
			_, err := n2.Write([]byte("hello"))
			if err != nil {
				return err
			}
			return n2.Close()
		})

		b, err := ioutil.ReadAll(n1)
		tt.success(err)

		_, err = n1.Read(nil)
		tt.eq(err, io.EOF)

		err = <-errs
		tt.success(err)

		tt.eq([]byte("hello"), b)
	})

	t.Run("netConn/BadMsg", func(t *testing.T) {
		tt := newTest(t)
		defer tt.done()

		c1, c2 := tt.pipe(nil, nil)

		n1 := websocket.NetConn(tt.ctx, c1, websocket.MessageBinary)
		n2 := websocket.NetConn(tt.ctx, c2, websocket.MessageText)

		errs := xsync.Go(func() error {
			_, err := n2.Write([]byte("hello"))
			if err != nil {
				return err
			}
			return nil
		})

		_, err := ioutil.ReadAll(n1)
		tt.errContains(err, `unexpected frame type read (expected MessageBinary): MessageText`)

		err = <-errs
		tt.success(err)
	})

	t.Run("wsjson", func(t *testing.T) {
		tt := newTest(t)
		defer tt.done()

		c1, c2 := tt.pipe(nil, nil)

		tt.goEchoLoop(c2)

		c1.SetReadLimit(1 << 30)

		exp := xrand.String(xrand.Int(131072))

		werr := xsync.Go(func() error {
			return wsjson.Write(tt.ctx, c1, exp)
		})

		var act interface{}
		err := wsjson.Read(tt.ctx, c1, &act)
		tt.success(err)
		tt.eq(exp, act)

		err = <-werr
		tt.success(err)

		err = c1.Close(websocket.StatusNormalClosure, "")
		tt.success(err)
	})

	t.Run("wspb", func(t *testing.T) {
		tt := newTest(t)
		defer tt.done()

		c1, c2 := tt.pipe(nil, nil)

		tt.goEchoLoop(c2)

		exp := ptypes.DurationProto(100)
		err := wspb.Write(tt.ctx, c1, exp)
		tt.success(err)

		act := &duration.Duration{}
		err = wspb.Read(tt.ctx, c1, act)
		tt.success(err)
		tt.eq(exp, act)

		err = c1.Close(websocket.StatusNormalClosure, "")
		tt.success(err)
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
			t.Errorf("echoLoop failed: %v", err)
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

type test struct {
	t   *testing.T
	ctx context.Context

	doneFuncs []func()
}

func newTest(t *testing.T) *test {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	tt := &test{t: t, ctx: ctx}
	tt.appendDone(cancel)
	return tt
}

func (tt *test) appendDone(f func()) {
	tt.doneFuncs = append(tt.doneFuncs, f)
}

func (tt *test) done() {
	for i := len(tt.doneFuncs) - 1; i >= 0; i-- {
		tt.doneFuncs[i]()
	}
}

func (tt *test) goEchoLoop(c *websocket.Conn) {
	ctx, cancel := context.WithCancel(tt.ctx)

	echoLoopErr := xsync.Go(func() error {
		err := wstest.EchoLoop(ctx, c)
		return assertCloseStatus(websocket.StatusNormalClosure, err)
	})
	tt.appendDone(func() {
		cancel()
		err := <-echoLoopErr
		if err != nil {
			tt.t.Errorf("echo loop error: %v", err)
		}
	})
}

func (tt *test) goDiscardLoop(c *websocket.Conn) {
	ctx, cancel := context.WithCancel(tt.ctx)

	discardLoopErr := xsync.Go(func() error {
		for {
			_, _, err := c.Read(ctx)
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				return nil
			}
			if err != nil {
				return err
			}
		}
	})
	tt.appendDone(func() {
		cancel()
		err := <-discardLoopErr
		if err != nil {
			tt.t.Errorf("discard loop error: %v", err)
		}
	})
}

func (tt *test) pipe(dialOpts *websocket.DialOptions, acceptOpts *websocket.AcceptOptions) (c1, c2 *websocket.Conn) {
	tt.t.Helper()

	c1, c2, err := wstest.Pipe(dialOpts, acceptOpts)
	if err != nil {
		tt.t.Fatal(err)
	}
	tt.appendDone(func() {
		c2.Close(websocket.StatusInternalError, "")
		c1.Close(websocket.StatusInternalError, "")
	})
	return c1, c2
}

func (tt *test) success(err error) {
	tt.t.Helper()
	if err != nil {
		tt.t.Fatal(err)
	}
}

func (tt *test) errContains(err error, sub string) {
	tt.t.Helper()
	if !cmp.ErrorContains(err, sub) {
		tt.t.Fatalf("error does not contain %q: %v", sub, err)
	}
}

func (tt *test) eq(exp, act interface{}) {
	tt.t.Helper()
	if !cmp.Equal(exp, act) {
		tt.t.Fatalf(cmp.Diff(exp, act))
	}
}
