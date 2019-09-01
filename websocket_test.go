package websocket_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"
	"go.uber.org/multierr"
	"golang.org/x/xerrors"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
	"nhooyr.io/websocket/wspb"
)

func TestHandshake(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		client func(ctx context.Context, url string) error
		server func(w http.ResponseWriter, r *http.Request) error
	}{
		{
			name: "badOrigin",
			server: func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r, nil)
				if err == nil {
					c.Close(websocket.StatusInternalError, "")
					return xerrors.New("expected error regarding bad origin")
				}
				if !strings.Contains(err.Error(), "not authorized") {
					return xerrors.Errorf("expected error regarding bad origin: %+v", err)
				}
				return nil
			},
			client: func(ctx context.Context, u string) error {
				h := http.Header{}
				h.Set("Origin", "http://unauthorized.com")
				c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
					HTTPHeader: h,
				})
				if err == nil {
					c.Close(websocket.StatusInternalError, "")
					return xerrors.New("expected handshake failure")
				}
				if !strings.Contains(err.Error(), "403") {
					return xerrors.Errorf("expected handshake failure: %+v", err)
				}
				return nil
			},
		},
		{
			name: "acceptSecureOrigin",
			server: func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r, nil)
				if err != nil {
					return err
				}
				c.Close(websocket.StatusNormalClosure, "")
				return nil
			},
			client: func(ctx context.Context, u string) error {
				h := http.Header{}
				h.Set("Origin", u)
				c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
					HTTPHeader: h,
				})
				if err != nil {
					return err
				}
				c.Close(websocket.StatusNormalClosure, "")
				return nil
			},
		},
		{
			name: "acceptInsecureOrigin",
			server: func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
					InsecureSkipVerify: true,
				})
				if err != nil {
					return err
				}
				c.Close(websocket.StatusNormalClosure, "")
				return nil
			},
			client: func(ctx context.Context, u string) error {
				h := http.Header{}
				h.Set("Origin", "https://example.com")
				c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
					HTTPHeader: h,
				})
				if err != nil {
					return err
				}
				c.Close(websocket.StatusNormalClosure, "")
				return nil
			},
		},
		{
			name: "cookies",
			server: func(w http.ResponseWriter, r *http.Request) error {
				cookie, err := r.Cookie("mycookie")
				if err != nil {
					return xerrors.Errorf("request is missing mycookie: %w", err)
				}
				if cookie.Value != "myvalue" {
					return xerrors.Errorf("expected %q but got %q", "myvalue", cookie.Value)
				}
				c, err := websocket.Accept(w, r, nil)
				if err != nil {
					return err
				}
				c.Close(websocket.StatusNormalClosure, "")
				return nil
			},
			client: func(ctx context.Context, u string) error {
				jar, err := cookiejar.New(nil)
				if err != nil {
					return xerrors.Errorf("failed to create cookie jar: %w", err)
				}
				parsedURL, err := url.Parse(u)
				if err != nil {
					return xerrors.Errorf("failed to parse url: %w", err)
				}
				parsedURL.Scheme = "http"
				jar.SetCookies(parsedURL, []*http.Cookie{
					{
						Name:  "mycookie",
						Value: "myvalue",
					},
				})
				hc := &http.Client{
					Jar: jar,
				}
				c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
					HTTPClient: hc,
				})
				if err != nil {
					return err
				}
				c.Close(websocket.StatusNormalClosure, "")
				return nil
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s, closeFn := testServer(t, tc.server, false)
			defer closeFn()

			wsURL := strings.Replace(s.URL, "http", "ws", 1)

			ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
			defer cancel()

			err := tc.client(ctx, wsURL)
			if err != nil {
				t.Fatalf("client failed: %+v", err)
			}
		})
	}
}

func TestConn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		acceptOpts *websocket.AcceptOptions
		server     func(ctx context.Context, c *websocket.Conn) error

		dialOpts *websocket.DialOptions
		response func(resp *http.Response) error
		client   func(ctx context.Context, c *websocket.Conn) error
	}{
		{
			name: "handshake",
			acceptOpts: &websocket.AcceptOptions{
				Subprotocols: []string{"myproto"},
			},
			dialOpts: &websocket.DialOptions{
				Subprotocols: []string{"myproto"},
			},
			response: func(resp *http.Response) error {
				headers := map[string]string{
					"Connection":             "Upgrade",
					"Upgrade":                "websocket",
					"Sec-WebSocket-Protocol": "myproto",
				}
				for h, exp := range headers {
					value := resp.Header.Get(h)
					err := assertEqualf(exp, value, "unexpected value for header %v", h)
					if err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			name: "handshake/defaultSubprotocol",
			server: func(ctx context.Context, c *websocket.Conn) error {
				return assertSubprotocol(c, "")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				return assertSubprotocol(c, "")
			},
		},
		{
			name: "handshake/subprotocolPriority",
			acceptOpts: &websocket.AcceptOptions{
				Subprotocols: []string{"echo", "lar"},
			},
			server: func(ctx context.Context, c *websocket.Conn) error {
				return assertSubprotocol(c, "echo")
			},
			dialOpts: &websocket.DialOptions{
				Subprotocols: []string{"poof", "echo"},
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				return assertSubprotocol(c, "echo")
			},
		},
		{
			name: "closeError",
			server: func(ctx context.Context, c *websocket.Conn) error {
				return wsjson.Write(ctx, c, "hello")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				err := assertJSONRead(ctx, c, "hello")
				if err != nil {
					return err
				}

				_, _, err = c.Reader(ctx)
				return assertCloseStatus(err, websocket.StatusInternalError)
			},
		},
		{
			name: "netConn",
			server: func(ctx context.Context, c *websocket.Conn) error {
				nc := websocket.NetConn(c, websocket.MessageBinary)
				defer nc.Close()

				nc.SetWriteDeadline(time.Time{})
				time.Sleep(1)
				nc.SetWriteDeadline(time.Now().Add(time.Second * 15))

				err := assertEqualf(websocket.Addr{}, nc.LocalAddr(), "net conn local address is not equal to websocket.Addr")
				if err != nil {
					return err
				}
				err = assertEqualf(websocket.Addr{}, nc.RemoteAddr(), "net conn remote address is not equal to websocket.Addr")
				if err != nil {
					return err
				}

				for i := 0; i < 3; i++ {
					_, err := nc.Write([]byte("hello"))
					if err != nil {
						return err
					}
				}

				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				nc := websocket.NetConn(c, websocket.MessageBinary)

				nc.SetReadDeadline(time.Time{})
				time.Sleep(1)
				nc.SetReadDeadline(time.Now().Add(time.Second * 15))

				for i := 0; i < 3; i++ {
					err := assertNetConnRead(nc, "hello")
					if err != nil {
						return err
					}
				}

				// Ensure the close frame is converted to an EOF and multiple read's after all return EOF.
				err2 := assertNetConnRead(nc, "hello")
				err := assertEqualf(io.EOF, err2, "unexpected error")
				if err != nil {
					return err
				}

				err2 = assertNetConnRead(nc, "hello")
				return assertEqualf(io.EOF, err2, "unexpected error")
			},
		},
		{
			name: "netConn/badReadMsgType",
			server: func(ctx context.Context, c *websocket.Conn) error {
				nc := websocket.NetConn(c, websocket.MessageBinary)

				nc.SetDeadline(time.Now().Add(time.Second * 15))

				_, err := nc.Read(make([]byte, 1))
				return assertErrorContains(err, "unexpected frame type")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				err := wsjson.Write(ctx, c, "meow")
				if err != nil {
					return err
				}

				_, _, err = c.Read(ctx)
				return assertCloseStatus(err, websocket.StatusUnsupportedData)
			},
		},
		{
			name: "netConn/badRead",
			server: func(ctx context.Context, c *websocket.Conn) error {
				nc := websocket.NetConn(c, websocket.MessageBinary)
				defer nc.Close()

				nc.SetDeadline(time.Now().Add(time.Second * 15))

				_, err2 := nc.Read(make([]byte, 1))
				err := assertCloseStatus(err2, websocket.StatusBadGateway)
				if err != nil {
					return err
				}

				_, err2 = nc.Write([]byte{0xff})
				return assertErrorContains(err2, "websocket closed")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				return c.Close(websocket.StatusBadGateway, "")
			},
		},
		{
			name: "wsjson/echo",
			server: func(ctx context.Context, c *websocket.Conn) error {
				return wsjson.Write(ctx, c, "meow")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				return assertJSONRead(ctx, c, "meow")
			},
		},
		{
			name: "protobuf/echo",
			server: func(ctx context.Context, c *websocket.Conn) error {
				return wspb.Write(ctx, c, ptypes.DurationProto(100))
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				return assertProtobufRead(ctx, c, ptypes.DurationProto(100))
			},
		},
		{
			name: "ping",
			server: func(ctx context.Context, c *websocket.Conn) error {
				ctx = c.CloseRead(ctx)

				err := c.Ping(ctx)
				if err != nil {
					return err
				}

				err = wsjson.Write(ctx, c, "hi")
				if err != nil {
					return err
				}

				<-ctx.Done()
				err = c.Ping(context.Background())
				return assertCloseStatus(err, websocket.StatusNormalClosure)
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				// We read a message from the connection and then keep reading until
				// the Ping completes.
				pingErrc := make(chan error, 1)
				go func() {
					pingErrc <- c.Ping(ctx)
				}()

				// Once this completes successfully, that means they sent their ping and we responded to it.
				err := assertJSONRead(ctx, c, "hi")
				if err != nil {
					return err
				}

				// Now we need to ensure we're reading for their pong from our ping.
				// Need new var to not race with above goroutine.
				ctx2 := c.CloseRead(ctx)

				// Now we wait for our pong.
				select {
				case err = <-pingErrc:
					return err
				case <-ctx2.Done():
					return xerrors.Errorf("failed to wait for pong: %w", ctx2.Err())
				}
			},
		},
		{
			name: "readLimit",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err2 := c.Read(ctx)
				return assertErrorContains(err2, "read limited at 32768 bytes")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				err := c.Write(ctx, websocket.MessageBinary, []byte(strings.Repeat("x", 32769)))
				if err != nil {
					return err
				}

				_, _, err2 := c.Read(ctx)
				return assertCloseStatus(err2, websocket.StatusMessageTooBig)
			},
		},
		{
			name: "wsjson/binary",
			server: func(ctx context.Context, c *websocket.Conn) error {
				var v interface{}
				err2 := wsjson.Read(ctx, c, &v)
				return assertErrorContains(err2, "unexpected frame type")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				return wspb.Write(ctx, c, ptypes.DurationProto(100))
			},
		},
		{
			name: "wsjson/badRead",
			server: func(ctx context.Context, c *websocket.Conn) error {
				var v interface{}
				err2 := wsjson.Read(ctx, c, &v)
				return assertErrorContains(err2, "failed to unmarshal json")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				return c.Write(ctx, websocket.MessageText, []byte("notjson"))
			},
		},
		{
			name: "wsjson/badWrite",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err2 := c.Read(ctx)
				return assertCloseStatus(err2, websocket.StatusNormalClosure)
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				err := wsjson.Write(ctx, c, fmt.Println)
				return assertErrorContains(err, "failed to encode json")
			},
		},
		{
			name: "wspb/text",
			server: func(ctx context.Context, c *websocket.Conn) error {
				var v proto.Message
				err := wspb.Read(ctx, c, v)
				return assertErrorContains(err, "unexpected frame type")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				return wsjson.Write(ctx, c, "hi")
			},
		},
		{
			name: "wspb/badRead",
			server: func(ctx context.Context, c *websocket.Conn) error {
				var v timestamp.Timestamp
				err := wspb.Read(ctx, c, &v)
				return assertErrorContains(err, "failed to unmarshal protobuf")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				return c.Write(ctx, websocket.MessageBinary, []byte("notpb"))
			},
		},
		{
			name: "wspb/badWrite",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				return assertCloseStatus(err, websocket.StatusNormalClosure)
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				err := wspb.Write(ctx, c, nil)
				return assertErrorIs(proto.ErrNil, err)
			},
		},
		{
			name: "badClose",
			server: func(ctx context.Context, c *websocket.Conn) error {
				return c.Close(9999, "")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				return assertCloseStatus(err, websocket.StatusInternalError)
			},
		},
		{
			name: "pingTimeout",
			server: func(ctx context.Context, c *websocket.Conn) error {
				ctx, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()
				err := c.Ping(ctx)
				return assertErrorIs(context.DeadlineExceeded, err)
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				err1 := assertErrorContains(err, "connection reset")
				err2 := assertErrorIs(io.EOF, err)
				if err1 != nil || err2 != nil {
					return nil
				}
				return multierr.Combine(err1, err2)
			},
		},
		{
			name: "writeTimeout",
			server: func(ctx context.Context, c *websocket.Conn) error {
				c.Writer(ctx, websocket.MessageBinary)

				ctx, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()
				err := c.Write(ctx, websocket.MessageBinary, []byte("meow"))
				return assertErrorIs(context.DeadlineExceeded, err)
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				return assertErrorIs(io.EOF, err)
			},
		},
		{
			name: "readTimeout",
			server: func(ctx context.Context, c *websocket.Conn) error {
				ctx, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()
				_, _, err := c.Read(ctx)
				return assertErrorIs(context.DeadlineExceeded, err)
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				return assertErrorIs(io.EOF, err)
			},
		},
		{
			name: "badOpCode",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, err := c.WriteFrame(ctx, true, 13, []byte("meow"))
				if err != nil {
					return err
				}
				_, _, err = c.Read(ctx)
				return assertErrorContains(err, "unknown opcode")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				return assertErrorContains(err, "unknown opcode")
			},
		},
		{
			name: "noRsv",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, err := c.WriteFrame(ctx, true, 99, []byte("meow"))
				if err != nil {
					return err
				}
				_, _, err = c.Read(ctx)
				cerr := &websocket.CloseError{}
				if !xerrors.As(err, cerr) || cerr.Code != websocket.StatusProtocolError {
					return xerrors.Errorf("expected close error with StatusProtocolError: %+v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				if err == nil || !strings.Contains(err.Error(), "rsv") {
					return xerrors.Errorf("expected error that contains rsv: %+v", err)
				}
				return nil
			},
		},
		{
			name: "largeControlFrame",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, err := c.WriteFrame(ctx, true, websocket.OpClose, []byte(strings.Repeat("x", 4096)))
				if err != nil {
					return err
				}
				_, _, err = c.Read(ctx)
				return assertCloseStatus(err, websocket.StatusProtocolError)
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				return assertErrorContains(err, "too large")
			},
		},
		{
			name: "fragmentedControlFrame",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, err := c.WriteFrame(ctx, false, websocket.OpPing, []byte(strings.Repeat("x", 32)))
				if err != nil {
					return err
				}
				err = c.Flush()
				if err != nil {
					return err
				}
				_, _, err = c.Read(ctx)
				return assertCloseStatus(err, websocket.StatusProtocolError)
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				return assertErrorContains(err, "fragmented")
			},
		},
		{
			name: "invalidClosePayload",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, err := c.WriteFrame(ctx, true, websocket.OpClose, []byte{0x17, 0x70})
				if err != nil {
					return err
				}
				_, _, err = c.Read(ctx)
				return assertCloseStatus(err, websocket.StatusProtocolError)
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				return assertErrorContains(err, "invalid status code")
			},
		},
		{
			name: "doubleReader",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, r, err := c.Reader(ctx)
				if err != nil {
					return err
				}
				p := make([]byte, 10)
				_, err = io.ReadFull(r, p)
				if err != nil {
					return err
				}
				_, _, err = c.Reader(ctx)
				return assertErrorContains(err, "previous message not read to completion")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				err := c.Write(ctx, websocket.MessageBinary, []byte(strings.Repeat("x", 11)))
				if err != nil {
					return err
				}
				_, _, err = c.Read(ctx)
				return assertCloseStatus(err, websocket.StatusInternalError)
			},
		},
		{
			name: "doubleFragmentedReader",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, r, err := c.Reader(ctx)
				if err != nil {
					return err
				}
				p := make([]byte, 10)
				_, err = io.ReadFull(r, p)
				if err != nil {
					return err
				}
				_, _, err = c.Reader(ctx)
				return assertErrorContains(err, "previous message not read to completion")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				w, err := c.Writer(ctx, websocket.MessageBinary)
				if err != nil {
					return err
				}
				_, err = w.Write([]byte(strings.Repeat("x", 10)))
				if err != nil {
					return xerrors.Errorf("expected non nil error")
				}
				err = c.Flush()
				if err != nil {
					return xerrors.Errorf("failed to flush: %w", err)
				}
				_, err = w.Write([]byte(strings.Repeat("x", 10)))
				if err != nil {
					return xerrors.Errorf("expected non nil error")
				}
				err = c.Flush()
				if err != nil {
					return xerrors.Errorf("failed to flush: %w", err)
				}
				_, _, err = c.Read(ctx)
				return assertCloseStatus(err, websocket.StatusInternalError)
			},
		},
		{
			name: "newMessageInFragmentedMessage",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, r, err := c.Reader(ctx)
				if err != nil {
					return err
				}
				p := make([]byte, 10)
				_, err = io.ReadFull(r, p)
				if err != nil {
					return err
				}
				_, _, err = c.Reader(ctx)
				return assertErrorContains(err, "received new data message without finishing")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				w, err := c.Writer(ctx, websocket.MessageBinary)
				if err != nil {
					return err
				}
				_, err = w.Write([]byte(strings.Repeat("x", 10)))
				if err != nil {
					return xerrors.Errorf("expected non nil error")
				}
				err = c.Flush()
				if err != nil {
					return xerrors.Errorf("failed to flush: %w", err)
				}
				_, err = c.WriteFrame(ctx, true, websocket.OpBinary, []byte(strings.Repeat("x", 10)))
				if err != nil {
					return xerrors.Errorf("expected non nil error")
				}
				_, _, err = c.Read(ctx)
				return assertErrorContains(err, "received new data message without finishing")
			},
		},
		{
			name: "continuationFrameWithoutDataFrame",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Reader(ctx)
				return assertErrorContains(err, "received continuation frame not after data")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, err := c.WriteFrame(ctx, false, websocket.OpContinuation, []byte(strings.Repeat("x", 10)))
				return err
			},
		},
		{
			name: "readBeforeEOF",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, r, err := c.Reader(ctx)
				if err != nil {
					return err
				}
				var v interface{}
				d := json.NewDecoder(r)
				err = d.Decode(&v)
				if err != nil {
					return err
				}
				err = assertEqualf("hi", v, "unexpected JSON")
				if err != nil {
					return err
				}
				_, b, err := c.Read(ctx)
				if err != nil {
					return err
				}
				return assertEqualf("hi", string(b), "unexpected JSON")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				err := wsjson.Write(ctx, c, "hi")
				if err != nil {
					return err
				}
				return c.Write(ctx, websocket.MessageText, []byte("hi"))
			},
		},
		{
			name: "newMessageInFragmentedMessage2",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, r, err := c.Reader(ctx)
				if err != nil {
					return err
				}
				p := make([]byte, 11)
				_, err = io.ReadFull(r, p)
				return assertErrorContains(err, "received new data message without finishing")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				w, err := c.Writer(ctx, websocket.MessageBinary)
				if err != nil {
					return err
				}
				_, err = w.Write([]byte(strings.Repeat("x", 10)))
				if err != nil {
					return xerrors.Errorf("expected non nil error")
				}
				err = c.Flush()
				if err != nil {
					return xerrors.Errorf("failed to flush: %w", err)
				}
				_, err = c.WriteFrame(ctx, true, websocket.OpBinary, []byte(strings.Repeat("x", 10)))
				if err != nil {
					return xerrors.Errorf("expected non nil error")
				}
				_, _, err = c.Read(ctx)
				return assertCloseStatus(err, websocket.StatusProtocolError)
			},
		},
		{
			name: "doubleRead",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, r, err := c.Reader(ctx)
				if err != nil {
					return err
				}
				_, err = ioutil.ReadAll(r)
				if err != nil {
					return err
				}
				_, err = r.Read(make([]byte, 1))
				return assertErrorContains(err, "cannot use EOFed reader")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				return c.Write(ctx, websocket.MessageBinary, []byte("hi"))
			},
		},
		{
			name: "eofInPayload",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				return assertErrorContains(err, "failed to read frame payload")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, err := c.WriteHalfFrame(ctx)
				if err != nil {
					return err
				}
				c.CloseUnderlyingConn()
				return nil
			},
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Run random tests over TLS.
			tls := rand.Intn(2) == 1

			s, closeFn := testServer(t, func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r, tc.acceptOpts)
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")
				if tc.server == nil {
					return nil
				}
				return tc.server(r.Context(), c)
			}, tls)
			defer closeFn()

			wsURL := strings.Replace(s.URL, "http", "ws", 1)

			ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
			defer cancel()

			opts := tc.dialOpts
			if tls {
				if opts == nil {
					opts = &websocket.DialOptions{}
				}
				opts.HTTPClient = s.Client()
			}

			c, resp, err := websocket.Dial(ctx, wsURL, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close(websocket.StatusInternalError, "")

			if tc.response != nil {
				err = tc.response(resp)
				if err != nil {
					t.Fatalf("response asserter failed: %+v", err)
				}
			}

			if tc.client != nil {
				err = tc.client(ctx, c)
				if err != nil {
					t.Fatalf("client failed: %+v", err)
				}
			}

			c.Close(websocket.StatusNormalClosure, "")
		})
	}
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

func testServer(tb testing.TB, fn func(w http.ResponseWriter, r *http.Request) error, tls bool) (s *httptest.Server, closeFn func()) {
	var conns int64
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&conns, 1)
		defer atomic.AddInt64(&conns, -1)

		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()

		r = r.WithContext(ctx)

		err := fn(w, r)
		if err != nil {
			tb.Errorf("server failed: %+v", err)
		}
	})
	if tls {
		s = httptest.NewTLSServer(h)
	} else {
		s = httptest.NewServer(h)
	}
	return s, func() {
		s.Close()

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		for atomic.LoadInt64(&conns) > 0 {
			if ctx.Err() != nil {
				tb.Fatalf("waiting for server to come down timed out: %v", ctx.Err())
			}
		}
	}
}

func TestAutobahn(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, name string, fn func(ctx context.Context, c *websocket.Conn) error) {
		run2 := func(t *testing.T, testingClient bool) {
			// Run random tests over TLS.
			tls := rand.Intn(2) == 1

			s, closeFn := testServer(t, func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
					Subprotocols: []string{"echo"},
				})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")
				c.SetReadLimit(1 << 40)

				ctx := r.Context()
				if testingClient {
					echoLoop(r.Context(), c)
					return nil
				}

				err = fn(ctx, c)
				if err != nil {
					return err
				}
				c.Close(websocket.StatusNormalClosure, "")
				return nil
			}, tls)
			defer closeFn()

			wsURL := strings.Replace(s.URL, "http", "ws", 1)

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()

			opts := &websocket.DialOptions{
				Subprotocols: []string{"echo"},
			}
			if tls {
				opts.HTTPClient = s.Client()
			}

			c, _, err := websocket.Dial(ctx, wsURL, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close(websocket.StatusInternalError, "")
			c.SetReadLimit(1 << 40)

			if testingClient {
				err = fn(ctx, c)
				if err != nil {
					t.Fatalf("client failed: %+v", err)
				}
				c.Close(websocket.StatusNormalClosure, "")
				return
			}

			echoLoop(ctx, c)
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			t.Run("server", func(t *testing.T) {
				t.Parallel()
				run2(t, false)
			})
			t.Run("client", func(t *testing.T) {
				t.Parallel()
				run2(t, true)
			})
		})
	}

	// Section 1.
	t.Run("echo", func(t *testing.T) {
		t.Parallel()

		lengths := []int{
			0,
			125,
			126,
			127,
			128,
			65535,
			65536,
			65536,
		}
		run := func(typ websocket.MessageType) {
			for i, l := range lengths {
				l := l
				run(t, fmt.Sprintf("%v/%v", typ, l), func(ctx context.Context, c *websocket.Conn) error {
					p := randBytes(l)
					if i == len(lengths)-1 {
						w, err := c.Writer(ctx, typ)
						if err != nil {
							return err
						}
						for i := 0; i < l; {
							j := i + 997
							if j > l {
								j = l
							}
							_, err = w.Write(p[i:j])
							if err != nil {
								return err
							}

							i = j
						}

						err = w.Close()
						if err != nil {
							return err
						}
					} else {
						err := c.Write(ctx, typ, p)
						if err != nil {
							return err
						}
					}
					actTyp, p2, err := c.Read(ctx)
					if err != nil {
						return err
					}
					err = assertEqualf(typ, actTyp, "unexpected message type")
					if err != nil {
						return err
					}
					return assertEqualf(p, p2, "unexpected message")
				})
			}
		}

		run(websocket.MessageText)
		run(websocket.MessageBinary)
	})

	// Section 2.
	t.Run("pingPong", func(t *testing.T) {
		t.Parallel()

		run(t, "emptyPayload", func(ctx context.Context, c *websocket.Conn) error {
			ctx = c.CloseRead(ctx)
			return c.PingWithPayload(ctx, "")
		})
		run(t, "smallTextPayload", func(ctx context.Context, c *websocket.Conn) error {
			ctx = c.CloseRead(ctx)
			return c.PingWithPayload(ctx, "hi")
		})
		run(t, "smallBinaryPayload", func(ctx context.Context, c *websocket.Conn) error {
			ctx = c.CloseRead(ctx)
			p := bytes.Repeat([]byte{0xFE}, 16)
			return c.PingWithPayload(ctx, string(p))
		})
		run(t, "largeBinaryPayload", func(ctx context.Context, c *websocket.Conn) error {
			ctx = c.CloseRead(ctx)
			p := bytes.Repeat([]byte{0xFE}, 125)
			return c.PingWithPayload(ctx, string(p))
		})
		run(t, "tooLargeBinaryPayload", func(ctx context.Context, c *websocket.Conn) error {
			c.CloseRead(ctx)
			p := bytes.Repeat([]byte{0xFE}, 126)
			err := c.PingWithPayload(ctx, string(p))
			return assertCloseStatus(err, websocket.StatusProtocolError)
		})
		run(t, "streamPingPayload", func(ctx context.Context, c *websocket.Conn) error {
			err := assertStreamPing(ctx, c, 125)
			if err != nil {
				return err
			}
			return assertCloseHandshake(ctx, c, websocket.StatusNormalClosure, "")
		})
		t.Run("unsolicitedPong", func(t *testing.T) {
			t.Parallel()

			var testCases = []struct {
				name        string
				pongPayload string
				ping        bool
			}{
				{
					name:        "noPayload",
					pongPayload: "",
				},
				{
					name:        "payload",
					pongPayload: "hi",
				},
				{
					name:        "pongThenPing",
					pongPayload: "hi",
					ping:        true,
				},
			}
			for _, tc := range testCases {
				tc := tc
				run(t, tc.name, func(ctx context.Context, c *websocket.Conn) error {
					_, err := c.WriteFrame(ctx, true, websocket.OpPong, []byte(tc.pongPayload))
					if err != nil {
						return err
					}
					if tc.ping {
						_, err := c.WriteFrame(ctx, true, websocket.OpPing, []byte("meow"))
						if err != nil {
							return err
						}
						err = assertReadFrame(ctx, c, websocket.OpPong, []byte("meow"))
						if err != nil {
							return err
						}
					}
					return assertCloseHandshake(ctx, c, websocket.StatusNormalClosure, "")
				})
			}
		})
		run(t, "tenPings", func(ctx context.Context, c *websocket.Conn) error {
			ctx = c.CloseRead(ctx)

			for i := 0; i < 10; i++ {
				err := c.Ping(ctx)
				if err != nil {
					return err
				}
			}

			_, err := c.WriteClose(ctx, websocket.StatusNormalClosure, "")
			if err != nil {
				return err
			}
			<-ctx.Done()

			err = c.Ping(context.Background())
			return assertCloseStatus(err, websocket.StatusNormalClosure)
		})
		run(t, "tenStreamedPings", func(ctx context.Context, c *websocket.Conn) error {
			for i := 0; i < 10; i++ {
				err := assertStreamPing(ctx, c, 125)
				if err != nil {
					return err
				}
			}

			return assertCloseHandshake(ctx, c, websocket.StatusNormalClosure, "")
		})
	})

	// Section 3.
	// We skip the per octet sending as it will add too much complexity.
	t.Run("reserved", func(t *testing.T) {
		t.Parallel()

		var testCases = []struct {
			name   string
			header websocket.Header
		}{
			{
				name: "rsv1",
				header: websocket.Header{
					Fin:           true,
					Rsv1:          true,
					OpCode:        websocket.OpClose,
					PayloadLength: 0,
				},
			},
			{
				name: "rsv2",
				header: websocket.Header{
					Fin:           true,
					Rsv2:          true,
					OpCode:        websocket.OpPong,
					PayloadLength: 0,
				},
			},
			{
				name: "rsv3",
				header: websocket.Header{
					Fin:           true,
					Rsv3:          true,
					OpCode:        websocket.OpBinary,
					PayloadLength: 0,
				},
			},
			{
				name: "rsvAll",
				header: websocket.Header{
					Fin:           true,
					Rsv1:          true,
					Rsv2:          true,
					Rsv3:          true,
					OpCode:        websocket.OpText,
					PayloadLength: 0,
				},
			},
		}
		for _, tc := range testCases {
			tc := tc
			run(t, tc.name, func(ctx context.Context, c *websocket.Conn) error {
				err := assertEcho(ctx, c, websocket.MessageText, 4096)
				if err != nil {
					return err
				}
				err = c.WriteHeader(ctx, tc.header)
				if err != nil {
					return err
				}
				err = c.Flush()
				if err != nil {
					return err
				}
				_, err = c.WriteFrame(ctx, true, websocket.OpPing, []byte("wtf"))
				if err != nil {
					return err
				}
				return assertReadCloseFrame(ctx, c, websocket.StatusProtocolError)
			})
		}
	})

	// Section 4.
	t.Run("opcodes", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name    string
			opcode  websocket.OpCode
			payload bool
			echo    bool
			ping    bool
		}{
			// Section 1.
			{
				name:   "3",
				opcode: 3,
			},
			{
				name:    "4",
				opcode:  4,
				payload: true,
			},
			{
				name:   "5",
				opcode: 5,
				echo:   true,
				ping:   true,
			},
			{
				name:    "6",
				opcode:  6,
				payload: true,
				echo:    true,
				ping:    true,
			},
			{
				name:    "7",
				opcode:  7,
				payload: true,
				echo:    true,
				ping:    true,
			},

			// Section 2.
			{
				name:   "11",
				opcode: 11,
			},
			{
				name:    "12",
				opcode:  12,
				payload: true,
			},
			{
				name:    "13",
				opcode:  13,
				payload: true,
				echo:    true,
				ping:    true,
			},
			{
				name:    "14",
				opcode:  14,
				payload: true,
				echo:    true,
				ping:    true,
			},
			{
				name:    "15",
				opcode:  15,
				payload: true,
				echo:    true,
				ping:    true,
			},
		}
		for _, tc := range testCases {
			tc := tc
			run(t, tc.name, func(ctx context.Context, c *websocket.Conn) error {
				if tc.echo {
					err := assertEcho(ctx, c, websocket.MessageText, 4096)
					if err != nil {
						return err
					}
				}

				p := []byte(nil)
				if tc.payload {
					p = randBytes(rand.Intn(4096) + 1)
				}
				_, err := c.WriteFrame(ctx, true, tc.opcode, p)
				if err != nil {
					return err
				}
				if tc.ping {
					_, err = c.WriteFrame(ctx, true, websocket.OpPing, []byte("wtf"))
					if err != nil {
						return err
					}
				}
				return assertReadCloseFrame(ctx, c, websocket.StatusProtocolError)
			})
		}
	})

	// Section 5.
	t.Run("fragmentation", func(t *testing.T) {
		t.Parallel()

		// 5.1 to 5.8
		testCases := []struct {
			name          string
			opcode        websocket.OpCode
			success       bool
			pingInBetween bool
		}{
			{
				name:    "ping",
				opcode:  websocket.OpPing,
				success: false,
			},
			{
				name:    "pong",
				opcode:  websocket.OpPong,
				success: false,
			},
			{
				name:    "text",
				opcode:  websocket.OpText,
				success: true,
			},
			{
				name:          "textPing",
				opcode:        websocket.OpText,
				success:       true,
				pingInBetween: true,
			},
		}
		for _, tc := range testCases {
			tc := tc
			run(t, tc.name, func(ctx context.Context, c *websocket.Conn) error {
				p1 := randBytes(16)
				_, err := c.WriteFrame(ctx, false, tc.opcode, p1)
				if err != nil {
					return err
				}
				err = c.BW().Flush()
				if err != nil {
					return err
				}
				if !tc.success {
					_, _, err = c.Read(ctx)
					return assertCloseStatus(err, websocket.StatusProtocolError)
				}

				if tc.pingInBetween {
					_, err = c.WriteFrame(ctx, true, websocket.OpPing, p1)
					if err != nil {
						return err
					}
				}

				p2 := randBytes(16)
				_, err = c.WriteFrame(ctx, true, websocket.OpContinuation, p2)
				if err != nil {
					return err
				}

				err = assertReadFrame(ctx, c, tc.opcode, p1)
				if err != nil {
					return err
				}

				if tc.pingInBetween {
					err = assertReadFrame(ctx, c, websocket.OpPong, p1)
					if err != nil {
						return err
					}
				}

				return assertReadFrame(ctx, c, websocket.OpContinuation, p2)
			})
		}

		t.Run("unexpectedContinuation", func(t *testing.T) {
			t.Parallel()

			testCases := []struct {
				name      string
				fin       bool
				textFirst bool
			}{
				{
					name: "fin",
					fin:  true,
				},
				{
					name: "noFin",
					fin:  false,
				},
				{
					name:      "echoFirst",
					fin:       false,
					textFirst: true,
				},
				// The rest of the tests in this section get complicated and do not inspire much confidence.
			}

			for _, tc := range testCases {
				tc := tc
				run(t, tc.name, func(ctx context.Context, c *websocket.Conn) error {
					if tc.textFirst {
						w, err := c.Writer(ctx, websocket.MessageText)
						if err != nil {
							return err
						}
						p1 := randBytes(32)
						_, err = w.Write(p1)
						if err != nil {
							return err
						}
						p2 := randBytes(32)
						_, err = w.Write(p2)
						if err != nil {
							return err
						}
						err = w.Close()
						if err != nil {
							return err
						}
						err = assertReadFrame(ctx, c, websocket.OpText, p1)
						if err != nil {
							return err
						}
						err = assertReadFrame(ctx, c, websocket.OpContinuation, p2)
						if err != nil {
							return err
						}
						err = assertReadFrame(ctx, c, websocket.OpContinuation, []byte{})
						if err != nil {
							return err
						}
					}

					_, err := c.WriteFrame(ctx, tc.fin, websocket.OpContinuation, randBytes(32))
					if err != nil {
						return err
					}
					err = c.BW().Flush()
					if err != nil {
						return err
					}

					return assertReadCloseFrame(ctx, c, websocket.StatusProtocolError)
				})
			}

			run(t, "doubleText", func(ctx context.Context, c *websocket.Conn) error {
				p1 := randBytes(32)
				_, err := c.WriteFrame(ctx, false, websocket.OpText, p1)
				if err != nil {
					return err
				}
				_, err = c.WriteFrame(ctx, true, websocket.OpText, randBytes(32))
				if err != nil {
					return err
				}
				err = assertReadFrame(ctx, c, websocket.OpText, p1)
				if err != nil {
					return err
				}
				return assertReadCloseFrame(ctx, c, websocket.StatusProtocolError)
			})

			run(t, "5.19", func(ctx context.Context, c *websocket.Conn) error {
				p1 := randBytes(32)
				p2 := randBytes(32)
				p3 := randBytes(32)
				p4 := randBytes(32)
				p5 := randBytes(32)

				_, err := c.WriteFrame(ctx, false, websocket.OpText, p1)
				if err != nil {
					return err
				}
				_, err = c.WriteFrame(ctx, false, websocket.OpContinuation, p2)
				if err != nil {
					return err
				}

				_, err = c.WriteFrame(ctx, true, websocket.OpPing, p1)
				if err != nil {
					return err
				}

				time.Sleep(time.Second)

				_, err = c.WriteFrame(ctx, false, websocket.OpContinuation, p3)
				if err != nil {
					return err
				}
				_, err = c.WriteFrame(ctx, false, websocket.OpContinuation, p4)
				if err != nil {
					return err
				}

				_, err = c.WriteFrame(ctx, true, websocket.OpPing, p1)
				if err != nil {
					return err
				}

				_, err = c.WriteFrame(ctx, true, websocket.OpContinuation, p5)
				if err != nil {
					return err
				}

				err = assertReadFrame(ctx, c, websocket.OpText, p1)
				if err != nil {
					return err
				}
				err = assertReadFrame(ctx, c, websocket.OpContinuation, p2)
				if err != nil {
					return err
				}
				err = assertReadFrame(ctx, c, websocket.OpPong, p1)
				if err != nil {
					return err
				}
				err = assertReadFrame(ctx, c, websocket.OpContinuation, p3)
				if err != nil {
					return err
				}
				err = assertReadFrame(ctx, c, websocket.OpContinuation, p4)
				if err != nil {
					return err
				}
				err = assertReadFrame(ctx, c, websocket.OpPong, p1)
				if err != nil {
					return err
				}
				err = assertReadFrame(ctx, c, websocket.OpContinuation, p5)
				if err != nil {
					return err
				}
				err = assertReadFrame(ctx, c, websocket.OpContinuation, []byte{})
				if err != nil {
					return err
				}
				return assertCloseHandshake(ctx, c, websocket.StatusNormalClosure, "")
			})
		})
	})

	// Section 7
	t.Run("closeHandling", func(t *testing.T) {
		t.Parallel()

		// 1.1 - 1.4 is useless.
		run(t, "1.5", func(ctx context.Context, c *websocket.Conn) error {
			p1 := randBytes(32)
			_, err := c.WriteFrame(ctx, false, websocket.OpText, p1)
			if err != nil {
				return err
			}
			err = c.Flush()
			if err != nil {
				return err
			}
			_, err = c.WriteClose(ctx, websocket.StatusNormalClosure, "")
			if err != nil {
				return err
			}
			err = assertReadFrame(ctx, c, websocket.OpText, p1)
			if err != nil {
				return err
			}
			return assertReadCloseFrame(ctx, c, websocket.StatusNormalClosure)
		})

		run(t, "1.6", func(ctx context.Context, c *websocket.Conn) error {
			// 262144 bytes.
			p1 := randBytes(1 << 18)
			err := c.Write(ctx, websocket.MessageText, p1)
			if err != nil {
				return err
			}
			_, err = c.WriteClose(ctx, websocket.StatusNormalClosure, "")
			if err != nil {
				return err
			}
			err = assertReadMessage(ctx, c, websocket.MessageText, p1)
			if err != nil {
				return err
			}
			return assertReadCloseFrame(ctx, c, websocket.StatusNormalClosure)
		})

		run(t, "emptyClose", func(ctx context.Context, c *websocket.Conn) error {
			_, err := c.WriteFrame(ctx, true, websocket.OpClose, nil)
			if err != nil {
				return err
			}
			return assertReadFrame(ctx, c, websocket.OpClose, []byte{})
		})

		run(t, "badClose", func(ctx context.Context, c *websocket.Conn) error {
			_, err := c.WriteFrame(ctx, true, websocket.OpClose, []byte{1})
			if err != nil {
				return err
			}
			return assertReadCloseFrame(ctx, c, websocket.StatusProtocolError)
		})

		run(t, "noReason", func(ctx context.Context, c *websocket.Conn) error {
			return assertCloseHandshake(ctx, c, websocket.StatusNormalClosure, "")
		})

		run(t, "simpleReason", func(ctx context.Context, c *websocket.Conn) error {
			return assertCloseHandshake(ctx, c, websocket.StatusNormalClosure, randString(16))
		})

		run(t, "maxReason", func(ctx context.Context, c *websocket.Conn) error {
			return assertCloseHandshake(ctx, c, websocket.StatusNormalClosure, randString(123))
		})

		run(t, "tooBigReason", func(ctx context.Context, c *websocket.Conn) error {
			_, err := c.WriteFrame(ctx, true, websocket.OpClose,
				append([]byte{0x03, 0xE8}, randString(124)...),
			)
			if err != nil {
				return err
			}
			return assertReadCloseFrame(ctx, c, websocket.StatusProtocolError)
		})

		t.Run("validCloses", func(t *testing.T) {
			t.Parallel()

			codes := [...]websocket.StatusCode{
				1000,
				1001,
				1002,
				1003,
				1007,
				1008,
				1009,
				1010,
				1011,
				3000,
				3999,
				4000,
				4999,
			}
			for _, code := range codes {
				run(t, strconv.Itoa(int(code)), func(ctx context.Context, c *websocket.Conn) error {
					return assertCloseHandshake(ctx, c, code, randString(32))
				})
			}
		})

		t.Run("invalidCloseCodes", func(t *testing.T) {
			t.Parallel()

			codes := []websocket.StatusCode{
				0,
				999,
				1004,
				1005,
				1006,
				1016,
				1100,
				2000,
				2999,
				5000,
				65535,
			}
			for _, code := range codes {
				run(t, strconv.Itoa(int(code)), func(ctx context.Context, c *websocket.Conn) error {
					p := make([]byte, 2)
					binary.BigEndian.PutUint16(p, uint16(code))
					p = append(p, randBytes(32)...)
					_, err := c.WriteFrame(ctx, true, websocket.OpClose, p)
					if err != nil {
						return err
					}
					return assertReadCloseFrame(ctx, c, websocket.StatusProtocolError)
				})
			}
		})
	})

	// Section 9.
	t.Run("limits", func(t *testing.T) {
		t.Parallel()

		t.Run("unfragmentedEcho", func(t *testing.T) {
			t.Parallel()

			lengths := []int{
				1 << 16, // 65536
				1 << 18, // 262144
				// Anything higher is completely unnecessary.
			}

			for _, l := range lengths {
				l := l
				run(t, strconv.Itoa(l), func(ctx context.Context, c *websocket.Conn) error {
					return assertEcho(ctx, c, websocket.MessageBinary, l)
				})
			}
		})

		t.Run("fragmentedEcho", func(t *testing.T) {
			t.Parallel()

			fragments := []int{
				64,
				256,
				1 << 10,
				1 << 12,
				1 << 14,
				1 << 16,
				1 << 18,
			}

			for _, l := range fragments {
				fragmentLength := l
				run(t, strconv.Itoa(fragmentLength), func(ctx context.Context, c *websocket.Conn) error {
					w, err := c.Writer(ctx, websocket.MessageText)
					if err != nil {
						return err
					}
					b := randBytes(1 << 18)
					for i := 0; i < len(b); {
						j := i + fragmentLength
						if j > len(b) {
							j = len(b)
						}

						_, err = w.Write(b[i:j])
						if err != nil {
							return err
						}

						i = j
					}
					err = w.Close()
					if err != nil {
						return err
					}

					err = assertReadMessage(ctx, c, websocket.MessageText, b)
					if err != nil {
						return err
					}
					return assertCloseHandshake(ctx, c, websocket.StatusNormalClosure, "")
				})
			}
		})

		t.Run("latencyEcho", func(t *testing.T) {
			t.Parallel()

			lengths := []int{
				0,
				16,
				64,
			}

			for _, l := range lengths {
				l := l
				run(t, strconv.Itoa(l), func(ctx context.Context, c *websocket.Conn) error {
					for i := 0; i < 1000; i++ {
						err := assertEcho(ctx, c, websocket.MessageBinary, l)
						if err != nil {
							return err
						}
					}
					return nil
				})
			}
		})
	})
}

func echoLoop(ctx context.Context, c *websocket.Conn) {
	defer c.Close(websocket.StatusInternalError, "")

	c.SetReadLimit(1 << 40)

	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	b := make([]byte, 32768)
	echo := func() error {
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

		return nil
	}

	for {
		err := echo()
		if err != nil {
			return
		}
	}
}

func assertCloseStatus(err error, code websocket.StatusCode) error {
	var cerr websocket.CloseError
	if !xerrors.As(err, &cerr) {
		return xerrors.Errorf("no websocket close error in error chain: %+v", err)
	}
	return assertEqualf(code, cerr.Code, "unexpected status code")
}

func assertJSONRead(ctx context.Context, c *websocket.Conn, exp interface{}) (err error) {
	var act interface{}
	err = wsjson.Read(ctx, c, &act)
	if err != nil {
		return err
	}

	return assertEqualf(exp, act, "unexpected JSON")
}

func randBytes(n int) []byte {
	return make([]byte, n)
}

func randString(n int) string {
	return string(randBytes(n))
}

func assertEcho(ctx context.Context, c *websocket.Conn, typ websocket.MessageType, n int) error {
	p := randBytes(n)
	err := c.Write(ctx, typ, p)
	if err != nil {
		return err
	}
	typ2, p2, err := c.Read(ctx)
	if err != nil {
		return err
	}
	err = assertEqualf(typ, typ2, "unexpected data type")
	if err != nil {
		return err
	}
	return assertEqualf(p, p2, "unexpected payload")
}

func assertProtobufRead(ctx context.Context, c *websocket.Conn, exp interface{}) error {
	expType := reflect.TypeOf(exp)
	actv := reflect.New(expType.Elem())
	act := actv.Interface().(proto.Message)
	err := wspb.Read(ctx, c, act)
	if err != nil {
		return err
	}

	return assertEqualf(exp, act, "unexpected protobuf")
}

func assertSubprotocol(c *websocket.Conn, exp string) error {
	return assertEqualf(exp, c.Subprotocol(), "unexpected subprotocol")
}

func assertEqualf(exp, act interface{}, f string, v ...interface{}) error {
	if diff := cmpDiff(exp, act); diff != "" {
		return xerrors.Errorf(f+": %v", append(v, diff)...)
	}
	return nil
}

func assertNetConnRead(r io.Reader, exp string) error {
	act := make([]byte, len(exp))
	_, err := r.Read(act)
	if err != nil {
		return err
	}
	return assertEqualf(exp, string(act), "unexpected net conn read")
}

func assertErrorContains(err error, exp string) error {
	if err == nil || !strings.Contains(err.Error(), exp) {
		return xerrors.Errorf("expected error that contains %q but got: %+v", exp, err)
	}
	return nil
}

func assertErrorIs(exp, act error) error {
	if !xerrors.Is(act, exp) {
		return xerrors.Errorf("expected error %+v to be in %+v", exp, act)
	}
	return nil
}

func assertReadFrame(ctx context.Context, c *websocket.Conn, opcode websocket.OpCode, p []byte) error {
	actOpcode, actP, err := c.ReadFrame(ctx)
	if err != nil {
		return err
	}
	err = assertEqualf(opcode, actOpcode, "unexpected frame opcode with payload %q", actP)
	if err != nil {
		return err
	}
	return assertEqualf(p, actP, "unexpected frame %v payload", opcode)
}

func assertReadCloseFrame(ctx context.Context, c *websocket.Conn, code websocket.StatusCode) error {
	actOpcode, actP, err := c.ReadFrame(ctx)
	if err != nil {
		return err
	}
	err = assertEqualf(websocket.OpClose, actOpcode, "unexpected frame opcode with payload %q", actP)
	if err != nil {
		return err
	}
	ce, err := websocket.ParseClosePayload(actP)
	if err != nil {
		return xerrors.Errorf("failed to parse close frame payload: %w", err)
	}
	return assertEqualf(ce.Code, code, "unexpected frame close frame code with payload %q", actP)
}

func assertCloseHandshake(ctx context.Context, c *websocket.Conn, code websocket.StatusCode, reason string) error {
	p, err := c.WriteClose(ctx, code, reason)
	if err != nil {
		return err
	}
	return assertReadFrame(ctx, c, websocket.OpClose, p)
}

func assertStreamPing(ctx context.Context, c *websocket.Conn, l int) error {
	err := c.WriteHeader(ctx, websocket.Header{
		Fin:           true,
		OpCode:        websocket.OpPing,
		PayloadLength: int64(l),
	})
	if err != nil {
		return err
	}
	for i := 0; i < l; i++ {
		err = c.BW().WriteByte(0xFE)
		if err != nil {
			return err
		}
		err = c.BW().Flush()
		if err != nil {
			return err
		}
	}
	return assertReadFrame(ctx, c, websocket.OpPong, bytes.Repeat([]byte{0xFE}, l))
}

func assertReadMessage(ctx context.Context, c *websocket.Conn, typ websocket.MessageType, p []byte) error {
	actTyp, actP, err := c.Read(ctx)
	if err != nil {
		return err
	}
	err = assertEqualf(websocket.MessageText, actTyp, "unexpected frame opcode with payload %q", actP)
	if err != nil {
		return err
	}
	return assertEqualf(p, actP, "unexpected frame %v payload", actTyp)
}
