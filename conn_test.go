//go:build !js

package websocket_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/errd"
	"nhooyr.io/websocket/internal/test/assert"
	"nhooyr.io/websocket/internal/test/wstest"
	"nhooyr.io/websocket/internal/test/xrand"
	"nhooyr.io/websocket/internal/xsync"
	"nhooyr.io/websocket/wsjson"
)

func TestConn(t *testing.T) {
	t.Parallel()

	t.Run("fuzzData", func(t *testing.T) {
		t.Parallel()

		compressionMode := func() websocket.CompressionMode {
			return websocket.CompressionMode(xrand.Int(int(websocket.CompressionContextTakeover) + 1))
		}

		for i := 0; i < 5; i++ {
			t.Run("", func(t *testing.T) {
				tt, c1, c2 := newConnTest(t, &websocket.DialOptions{
					CompressionMode:      compressionMode(),
					CompressionThreshold: xrand.Int(9999),
				}, &websocket.AcceptOptions{
					CompressionMode:      compressionMode(),
					CompressionThreshold: xrand.Int(9999),
				})

				tt.goEchoLoop(c2)

				c1.SetReadLimit(131072)

				for i := 0; i < 5; i++ {
					err := wstest.Echo(tt.ctx, c1, 131072)
					assert.Success(t, err)
				}

				err := c1.Close(websocket.StatusNormalClosure, "")
				assert.Success(t, err)
			})
		}
	})

	t.Run("badClose", func(t *testing.T) {
		tt, c1, c2 := newConnTest(t, nil, nil)

		c2.CloseRead(tt.ctx)

		err := c1.Close(-1, "")
		assert.Contains(t, err, "failed to marshal close frame: status code StatusCode(-1) cannot be set")
	})

	t.Run("ping", func(t *testing.T) {
		tt, c1, c2 := newConnTest(t, nil, nil)

		c1.CloseRead(tt.ctx)
		c2.CloseRead(tt.ctx)

		for i := 0; i < 10; i++ {
			err := c1.Ping(tt.ctx)
			assert.Success(t, err)
		}

		err := c1.Close(websocket.StatusNormalClosure, "")
		assert.Success(t, err)
	})

	t.Run("badPing", func(t *testing.T) {
		tt, c1, c2 := newConnTest(t, nil, nil)

		c2.CloseRead(tt.ctx)

		ctx, cancel := context.WithTimeout(tt.ctx, time.Millisecond*100)
		defer cancel()

		err := c1.Ping(ctx)
		assert.Contains(t, err, "failed to wait for pong")
	})

	t.Run("concurrentWrite", func(t *testing.T) {
		tt, c1, c2 := newConnTest(t, nil, nil)

		tt.goDiscardLoop(c2)

		msg := xrand.Bytes(xrand.Int(9999))
		const count = 100
		errs := make(chan error, count)

		for i := 0; i < count; i++ {
			go func() {
				select {
				case errs <- c1.Write(tt.ctx, websocket.MessageBinary, msg):
				case <-tt.ctx.Done():
					return
				}
			}()
		}

		for i := 0; i < count; i++ {
			select {
			case err := <-errs:
				assert.Success(t, err)
			case <-tt.ctx.Done():
				t.Fatal(tt.ctx.Err())
			}
		}

		err := c1.Close(websocket.StatusNormalClosure, "")
		assert.Success(t, err)
	})

	t.Run("concurrentWriteError", func(t *testing.T) {
		tt, c1, _ := newConnTest(t, nil, nil)

		_, err := c1.Writer(tt.ctx, websocket.MessageText)
		assert.Success(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)
		defer cancel()

		err = c1.Write(ctx, websocket.MessageText, []byte("x"))
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unexpected error: %#v", err)
		}
	})

	t.Run("netConn", func(t *testing.T) {
		tt, c1, c2 := newConnTest(t, nil, nil)

		n1 := websocket.NetConn(tt.ctx, c1, websocket.MessageBinary)
		n2 := websocket.NetConn(tt.ctx, c2, websocket.MessageBinary)

		// Does not give any confidence but at least ensures no crashes.
		d, _ := tt.ctx.Deadline()
		n1.SetDeadline(d)
		n1.SetDeadline(time.Time{})

		assert.Equal(t, "remote addr", n1.RemoteAddr(), n1.LocalAddr())
		assert.Equal(t, "remote addr string", "pipe", n1.RemoteAddr().String())
		assert.Equal(t, "remote addr network", "pipe", n1.RemoteAddr().Network())

		errs := xsync.Go(func() error {
			_, err := n2.Write([]byte("hello"))
			if err != nil {
				return err
			}
			return n2.Close()
		})

		b, err := io.ReadAll(n1)
		assert.Success(t, err)

		_, err = n1.Read(nil)
		assert.Equal(t, "read error", err, io.EOF)

		select {
		case err := <-errs:
			assert.Success(t, err)
		case <-tt.ctx.Done():
			t.Fatal(tt.ctx.Err())
		}

		assert.Equal(t, "read msg", []byte("hello"), b)
	})

	t.Run("netConn/BadMsg", func(t *testing.T) {
		tt, c1, c2 := newConnTest(t, nil, nil)

		n1 := websocket.NetConn(tt.ctx, c1, websocket.MessageBinary)
		n2 := websocket.NetConn(tt.ctx, c2, websocket.MessageText)

		c2.CloseRead(tt.ctx)
		errs := xsync.Go(func() error {
			_, err := n2.Write([]byte("hello"))
			return err
		})

		_, err := io.ReadAll(n1)
		assert.Contains(t, err, `unexpected frame type read (expected MessageBinary): MessageText`)

		select {
		case err := <-errs:
			assert.Success(t, err)
		case <-tt.ctx.Done():
			t.Fatal(tt.ctx.Err())
		}
	})

	t.Run("netConn/readLimit", func(t *testing.T) {
		tt, c1, c2 := newConnTest(t, nil, nil)

		n1 := websocket.NetConn(tt.ctx, c1, websocket.MessageBinary)
		n2 := websocket.NetConn(tt.ctx, c2, websocket.MessageBinary)

		s := strings.Repeat("papa", 1<<20)
		errs := xsync.Go(func() error {
			_, err := n2.Write([]byte(s))
			if err != nil {
				return err
			}
			return n2.Close()
		})

		b, err := io.ReadAll(n1)
		assert.Success(t, err)

		_, err = n1.Read(nil)
		assert.Equal(t, "read error", err, io.EOF)

		select {
		case err := <-errs:
			assert.Success(t, err)
		case <-tt.ctx.Done():
			t.Fatal(tt.ctx.Err())
		}

		assert.Equal(t, "read msg", s, string(b))
	})

	t.Run("netConn/pastDeadline", func(t *testing.T) {
		tt, c1, c2 := newConnTest(t, nil, nil)

		n1 := websocket.NetConn(tt.ctx, c1, websocket.MessageBinary)
		n2 := websocket.NetConn(tt.ctx, c2, websocket.MessageBinary)

		n1.SetDeadline(time.Now().Add(-time.Minute))
		n2.SetDeadline(time.Now().Add(-time.Minute))

		// No panic we're good.
	})

	t.Run("wsjson", func(t *testing.T) {
		tt, c1, c2 := newConnTest(t, nil, nil)

		tt.goEchoLoop(c2)

		c1.SetReadLimit(1 << 30)

		exp := xrand.String(xrand.Int(131072))

		werr := xsync.Go(func() error {
			return wsjson.Write(tt.ctx, c1, exp)
		})

		var act interface{}
		err := wsjson.Read(tt.ctx, c1, &act)
		assert.Success(t, err)
		assert.Equal(t, "read msg", exp, act)

		select {
		case err := <-werr:
			assert.Success(t, err)
		case <-tt.ctx.Done():
			t.Fatal(tt.ctx.Err())
		}

		err = c1.Close(websocket.StatusNormalClosure, "")
		assert.Success(t, err)
	})

	t.Run("HTTPClient.Timeout", func(t *testing.T) {
		tt, c1, c2 := newConnTest(t, &websocket.DialOptions{
			HTTPClient: &http.Client{Timeout: time.Second * 5},
		}, nil)

		tt.goEchoLoop(c2)

		c1.SetReadLimit(1 << 30)

		exp := xrand.String(xrand.Int(131072))

		werr := xsync.Go(func() error {
			return wsjson.Write(tt.ctx, c1, exp)
		})

		var act interface{}
		err := wsjson.Read(tt.ctx, c1, &act)
		assert.Success(t, err)
		assert.Equal(t, "read msg", exp, act)

		select {
		case err := <-werr:
			assert.Success(t, err)
		case <-tt.ctx.Done():
			t.Fatal(tt.ctx.Err())
		}

		err = c1.Close(websocket.StatusNormalClosure, "")
		assert.Success(t, err)
	})

	t.Run("CloseNow", func(t *testing.T) {
		_, c1, c2 := newConnTest(t, nil, nil)

		err1 := c1.CloseNow()
		err2 := c2.CloseNow()
		assert.Success(t, err1)
		assert.Success(t, err2)
		err1 = c1.CloseNow()
		err2 = c2.CloseNow()
		assert.ErrorIs(t, websocket.ErrClosed, err1)
		assert.ErrorIs(t, websocket.ErrClosed, err2)
	})

	t.Run("MidReadClose", func(t *testing.T) {
		tt, c1, c2 := newConnTest(t, nil, nil)

		tt.goEchoLoop(c2)

		c1.SetReadLimit(131072)

		for i := 0; i < 5; i++ {
			err := wstest.Echo(tt.ctx, c1, 131072)
			assert.Success(t, err)
		}

		err := wsjson.Write(tt.ctx, c1, "four")
		assert.Success(t, err)
		_, _, err = c1.Reader(tt.ctx)
		assert.Success(t, err)

		err = c1.Close(websocket.StatusNormalClosure, "")
		assert.Success(t, err)
	})
}

func TestWasm(t *testing.T) {
	t.Parallel()
	if os.Getenv("CI") == "" {
		t.SkipNow()
	}

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := echoServer(w, r, &websocket.AcceptOptions{
			Subprotocols:       []string{"echo"},
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Error(err)
		}
	}))
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "test", "-exec=wasmbrowsertest", ".", "-v")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", fmt.Sprintf("WS_ECHO_SERVER_URL=%v", s.URL))

	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wasm test binary failed: %v:\n%s", err, b)
	}
}

func assertCloseStatus(exp websocket.StatusCode, err error) error {
	if websocket.CloseStatus(err) == -1 {
		return fmt.Errorf("expected websocket.CloseError: %T %v", err, err)
	}
	if websocket.CloseStatus(err) != exp {
		return fmt.Errorf("expected close status %v but got %v", exp, err)
	}
	return nil
}

type connTest struct {
	t   testing.TB
	ctx context.Context
}

func newConnTest(t testing.TB, dialOpts *websocket.DialOptions, acceptOpts *websocket.AcceptOptions) (tt *connTest, c1, c2 *websocket.Conn) {
	if t, ok := t.(*testing.T); ok {
		t.Parallel()
	}
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	tt = &connTest{t: t, ctx: ctx}
	t.Cleanup(cancel)

	c1, c2 = wstest.Pipe(dialOpts, acceptOpts)
	if xrand.Bool() {
		c1, c2 = c2, c1
	}
	t.Cleanup(func() {
		c2.CloseNow()
		c1.CloseNow()
	})

	return tt, c1, c2
}

func (tt *connTest) goEchoLoop(c *websocket.Conn) {
	ctx, cancel := context.WithCancel(tt.ctx)

	echoLoopErr := xsync.Go(func() error {
		err := wstest.EchoLoop(ctx, c)
		return assertCloseStatus(websocket.StatusNormalClosure, err)
	})
	tt.t.Cleanup(func() {
		cancel()
		err := <-echoLoopErr
		if err != nil {
			tt.t.Errorf("echo loop error: %v", err)
		}
	})
}

func (tt *connTest) goDiscardLoop(c *websocket.Conn) {
	ctx, cancel := context.WithCancel(tt.ctx)

	discardLoopErr := xsync.Go(func() error {
		defer c.Close(websocket.StatusInternalError, "")

		for {
			_, _, err := c.Read(ctx)
			if err != nil {
				return assertCloseStatus(websocket.StatusNormalClosure, err)
			}
		}
	})
	tt.t.Cleanup(func() {
		cancel()
		err := <-discardLoopErr
		if err != nil {
			tt.t.Errorf("discard loop error: %v", err)
		}
	})
}

func BenchmarkConn(b *testing.B) {
	var benchCases = []struct {
		name string
		mode websocket.CompressionMode
	}{
		{
			name: "disabledCompress",
			mode: websocket.CompressionDisabled,
		},
		{
			name: "compressContextTakeover",
			mode: websocket.CompressionContextTakeover,
		},
		{
			name: "compressNoContext",
			mode: websocket.CompressionNoContextTakeover,
		},
	}
	for _, bc := range benchCases {
		b.Run(bc.name, func(b *testing.B) {
			bb, c1, c2 := newConnTest(b, &websocket.DialOptions{
				CompressionMode: bc.mode,
			}, &websocket.AcceptOptions{
				CompressionMode: bc.mode,
			})

			bb.goEchoLoop(c2)

			bytesWritten := c1.RecordBytesWritten()
			bytesRead := c1.RecordBytesRead()

			msg := []byte(strings.Repeat("1234", 128))
			readBuf := make([]byte, len(msg))
			writes := make(chan struct{})
			defer close(writes)
			werrs := make(chan error)

			go func() {
				for range writes {
					select {
					case werrs <- c1.Write(bb.ctx, websocket.MessageText, msg):
					case <-bb.ctx.Done():
						return
					}
				}
			}()
			b.SetBytes(int64(len(msg)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				select {
				case writes <- struct{}{}:
				case <-bb.ctx.Done():
					b.Fatal(bb.ctx.Err())
				}

				typ, r, err := c1.Reader(bb.ctx)
				if err != nil {
					b.Fatal(i, err)
				}
				if websocket.MessageText != typ {
					assert.Equal(b, "data type", websocket.MessageText, typ)
				}

				_, err = io.ReadFull(r, readBuf)
				if err != nil {
					b.Fatal(err)
				}

				n2, err := r.Read(readBuf)
				if err != io.EOF {
					assert.Equal(b, "read err", io.EOF, err)
				}
				if n2 != 0 {
					assert.Equal(b, "n2", 0, n2)
				}

				if !bytes.Equal(msg, readBuf) {
					assert.Equal(b, "msg", msg, readBuf)
				}

				select {
				case err = <-werrs:
				case <-bb.ctx.Done():
					b.Fatal(bb.ctx.Err())
				}
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()

			b.ReportMetric(float64(*bytesWritten/b.N), "written/op")
			b.ReportMetric(float64(*bytesRead/b.N), "read/op")

			err := c1.Close(websocket.StatusNormalClosure, "")
			assert.Success(b, err)
		})
	}
}

func echoServer(w http.ResponseWriter, r *http.Request, opts *websocket.AcceptOptions) (err error) {
	defer errd.Wrap(&err, "echo server failed")

	c, err := websocket.Accept(w, r, opts)
	if err != nil {
		return err
	}
	defer c.Close(websocket.StatusInternalError, "")

	err = wstest.EchoLoop(r.Context(), c)
	return assertCloseStatus(websocket.StatusNormalClosure, err)
}

func assertEcho(tb testing.TB, ctx context.Context, c *websocket.Conn) {
	exp := xrand.String(xrand.Int(131072))

	werr := xsync.Go(func() error {
		return wsjson.Write(ctx, c, exp)
	})

	var act interface{}
	c.SetReadLimit(1 << 30)
	err := wsjson.Read(ctx, c, &act)
	assert.Success(tb, err)
	assert.Equal(tb, "read msg", exp, act)

	select {
	case err := <-werr:
		assert.Success(tb, err)
	case <-ctx.Done():
		tb.Fatal(ctx.Err())
	}
}

func assertClose(tb testing.TB, c *websocket.Conn) {
	tb.Helper()
	err := c.Close(websocket.StatusNormalClosure, "")
	assert.Success(tb, err)
}

func TestConcurrentClosePing(t *testing.T) {
	t.Parallel()
	for i := 0; i < 64; i++ {
		func() {
			c1, c2 := wstest.Pipe(nil, nil)
			defer c1.CloseNow()
			defer c2.CloseNow()
			c1.CloseRead(context.Background())
			c2.CloseRead(context.Background())
			errc := xsync.Go(func() error {
				for range time.Tick(time.Millisecond) {
					err := c1.Ping(context.Background())
					if err != nil {
						return err
					}
				}
				panic("unreachable")
			})

			time.Sleep(10 * time.Millisecond)
			assert.Success(t, c1.Close(websocket.StatusNormalClosure, ""))
			<-errc
		}()
	}
}
