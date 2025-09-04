//go:build !js

package websocket_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/internal/errd"
	"github.com/coder/websocket/internal/test/assert"
	"github.com/coder/websocket/internal/test/wstest"
	"github.com/coder/websocket/internal/test/xrand"
	"github.com/coder/websocket/internal/xsync"
	"github.com/coder/websocket/wsjson"
)

func TestConn(t *testing.T) {
	t.Parallel()

	t.Run("fuzzData", func(t *testing.T) {
		t.Parallel()

		compressionMode := func() websocket.CompressionMode {
			return websocket.CompressionMode(xrand.Int(int(websocket.CompressionContextTakeover) + 1))
		}

		for range 5 {
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

				for range 5 {
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

		for range 10 {
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

	t.Run("pingReceivedPongReceived", func(t *testing.T) {
		var pingReceived1, pongReceived1 bool
		var pingReceived2, pongReceived2 bool
		tt, c1, c2 := newConnTest(t,
			&websocket.DialOptions{
				OnPingReceived: func(ctx context.Context, payload []byte) bool {
					pingReceived1 = true
					return true
				},
				OnPongReceived: func(ctx context.Context, payload []byte) {
					pongReceived1 = true
				},
			}, &websocket.AcceptOptions{
				OnPingReceived: func(ctx context.Context, payload []byte) bool {
					pingReceived2 = true
					return true
				},
				OnPongReceived: func(ctx context.Context, payload []byte) {
					pongReceived2 = true
				},
			},
		)

		c1.CloseRead(tt.ctx)
		c2.CloseRead(tt.ctx)

		ctx, cancel := context.WithTimeout(tt.ctx, time.Millisecond*100)
		defer cancel()

		err := c1.Ping(ctx)
		assert.Success(t, err)

		c1.CloseNow()
		c2.CloseNow()

		assert.Equal(t, "only one side receives the ping", false, pingReceived1 && pingReceived2)
		assert.Equal(t, "only one side receives the pong", false, pongReceived1 && pongReceived2)
		assert.Equal(t, "ping and pong received", true, (pingReceived1 && pongReceived2) || (pingReceived2 && pongReceived1))
	})

	t.Run("pingReceivedPongNotReceived", func(t *testing.T) {
		var pingReceived1, pongReceived1 bool
		var pingReceived2, pongReceived2 bool
		tt, c1, c2 := newConnTest(t,
			&websocket.DialOptions{
				OnPingReceived: func(ctx context.Context, payload []byte) bool {
					pingReceived1 = true
					return false
				},
				OnPongReceived: func(ctx context.Context, payload []byte) {
					pongReceived1 = true
				},
			}, &websocket.AcceptOptions{
				OnPingReceived: func(ctx context.Context, payload []byte) bool {
					pingReceived2 = true
					return false
				},
				OnPongReceived: func(ctx context.Context, payload []byte) {
					pongReceived2 = true
				},
			},
		)

		c1.CloseRead(tt.ctx)
		c2.CloseRead(tt.ctx)

		ctx, cancel := context.WithTimeout(tt.ctx, time.Millisecond*100)
		defer cancel()

		err := c1.Ping(ctx)
		assert.Contains(t, err, "failed to wait for pong")

		c1.CloseNow()
		c2.CloseNow()

		assert.Equal(t, "only one side receives the ping", false, pingReceived1 && pingReceived2)
		assert.Equal(t, "ping received and pong not received", true, (pingReceived1 && !pongReceived2) || (pingReceived2 && !pongReceived1))
	})

	t.Run("concurrentWrite", func(t *testing.T) {
		tt, c1, c2 := newConnTest(t, nil, nil)

		tt.goDiscardLoop(c2)

		msg := xrand.Bytes(xrand.Int(9999))
		const count = 100
		errs := make(chan error, count)

		for range count {
			go func() {
				select {
				case errs <- c1.Write(tt.ctx, websocket.MessageBinary, msg):
				case <-tt.ctx.Done():
					return
				}
			}()
		}

		for range count {
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

		var act any
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

		var act any
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

		for range 5 {
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

	t.Run("ReadLimitExceededReturnsErrMessageTooBig", func(t *testing.T) {
		tt, c1, c2 := newConnTest(t, nil, nil)

		c1.SetReadLimit(1024)
		_ = c2.CloseRead(tt.ctx)

		writeDone := xsync.Go(func() error {
			payload := strings.Repeat("x", 4096)
			return c2.Write(tt.ctx, websocket.MessageText, []byte(payload))
		})

		_, _, err := c1.Read(tt.ctx)
		assert.ErrorIs(t, websocket.ErrMessageTooBig, err)
		assert.Contains(t, err, "read limited at 1025 bytes")

		_ = c2.CloseNow()
		<-writeDone
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
	cmd.Env = append(cleanEnv(os.Environ()), "GOOS=js", "GOARCH=wasm", fmt.Sprintf("WS_ECHO_SERVER_URL=%v", s.URL))

	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wasm test binary failed: %v:\n%s", err, b)
	}
}

func cleanEnv(env []string) (out []string) {
	for _, e := range env {
		// Filter out GITHUB envs and anything with token in it,
		// especially GITHUB_TOKEN in CI as it breaks TestWasm.
		if strings.HasPrefix(e, "GITHUB") || strings.Contains(e, "TOKEN") {
			continue
		}
		out = append(out, e)
	}
	return out
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
	benchCases := []struct {
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

	var act any
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
	for range 64 {
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

func TestConnClosePropagation(t *testing.T) {
	t.Parallel()

	want := []byte("hello")
	keepWriting := func(c *websocket.Conn) <-chan error {
		return xsync.Go(func() error {
			for {
				err := c.Write(context.Background(), websocket.MessageText, want)
				if err != nil {
					return err
				}
			}
		})
	}
	keepReading := func(c *websocket.Conn) <-chan error {
		return xsync.Go(func() error {
			for {
				_, got, err := c.Read(context.Background())
				if err != nil {
					return err
				}
				if !bytes.Equal(want, got) {
					return fmt.Errorf("unexpected message: want %q, got %q", want, got)
				}
			}
		})
	}
	checkReadErr := func(t *testing.T, err error) {
		// Check read error (output depends on when read is called in relation to connection closure).
		var ce websocket.CloseError
		if errors.As(err, &ce) {
			assert.Equal(t, "", websocket.StatusNormalClosure, ce.Code)
		} else {
			assert.ErrorIs(t, net.ErrClosed, err)
		}
	}
	checkConnErrs := func(t *testing.T, conn ...*websocket.Conn) {
		for _, c := range conn {
			// Check write error.
			err := c.Write(context.Background(), websocket.MessageText, want)
			assert.ErrorIs(t, net.ErrClosed, err)

			_, _, err = c.Read(context.Background())
			checkReadErr(t, err)
		}
	}

	t.Run("CloseOtherSideDuringWrite", func(t *testing.T) {
		tt, this, other := newConnTest(t, nil, nil)

		_ = this.CloseRead(tt.ctx)
		thisWriteErr := keepWriting(this)

		_, got, err := other.Read(tt.ctx)
		assert.Success(t, err)
		assert.Equal(t, "msg", want, got)

		err = other.Close(websocket.StatusNormalClosure, "")
		assert.Success(t, err)

		select {
		case err := <-thisWriteErr:
			assert.ErrorIs(t, net.ErrClosed, err)
		case <-tt.ctx.Done():
			t.Fatal(tt.ctx.Err())
		}

		checkConnErrs(t, this, other)
	})
	t.Run("CloseThisSideDuringWrite", func(t *testing.T) {
		tt, this, other := newConnTest(t, nil, nil)

		_ = this.CloseRead(tt.ctx)
		thisWriteErr := keepWriting(this)
		otherReadErr := keepReading(other)

		err := this.Close(websocket.StatusNormalClosure, "")
		assert.Success(t, err)

		select {
		case err := <-thisWriteErr:
			assert.ErrorIs(t, net.ErrClosed, err)
		case <-tt.ctx.Done():
			t.Fatal(tt.ctx.Err())
		}

		select {
		case err := <-otherReadErr:
			checkReadErr(t, err)
		case <-tt.ctx.Done():
			t.Fatal(tt.ctx.Err())
		}

		checkConnErrs(t, this, other)
	})
	t.Run("CloseOtherSideDuringRead", func(t *testing.T) {
		tt, this, other := newConnTest(t, nil, nil)

		_ = other.CloseRead(tt.ctx)
		errs := keepReading(this)

		err := other.Write(tt.ctx, websocket.MessageText, want)
		assert.Success(t, err)

		err = other.Close(websocket.StatusNormalClosure, "")
		assert.Success(t, err)

		select {
		case err := <-errs:
			checkReadErr(t, err)
		case <-tt.ctx.Done():
			t.Fatal(tt.ctx.Err())
		}

		checkConnErrs(t, this, other)
	})
	t.Run("CloseThisSideDuringRead", func(t *testing.T) {
		tt, this, other := newConnTest(t, nil, nil)

		thisReadErr := keepReading(this)
		otherReadErr := keepReading(other)

		err := other.Write(tt.ctx, websocket.MessageText, want)
		assert.Success(t, err)

		err = this.Close(websocket.StatusNormalClosure, "")
		assert.Success(t, err)

		select {
		case err := <-thisReadErr:
			checkReadErr(t, err)
		case <-tt.ctx.Done():
			t.Fatal(tt.ctx.Err())
		}

		select {
		case err := <-otherReadErr:
			checkReadErr(t, err)
		case <-tt.ctx.Done():
			t.Fatal(tt.ctx.Err())
		}

		checkConnErrs(t, this, other)
	})
}
