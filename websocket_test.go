package websocket_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
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

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
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
				_, err := c.WriteFrame(ctx, true, websocket.OPClose, []byte(strings.Repeat("x", 4096)))
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
				_, err := c.WriteFrame(ctx, false, websocket.OPPing, []byte(strings.Repeat("x", 32)))
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
				_, err := c.WriteFrame(ctx, true, websocket.OPClose, []byte{0x17, 0x70})
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
				_, err = c.WriteFrame(ctx, true, websocket.OPBinary, []byte(strings.Repeat("x", 10)))
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
				_, err := c.WriteFrame(ctx, false, websocket.OPContinuation, []byte(strings.Repeat("x", 10)))
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
				_, err = c.WriteFrame(ctx, true, websocket.OPBinary, []byte(strings.Repeat("x", 10)))
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
				return err
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

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
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

		ctx, cancel := context.WithTimeout(r.Context(), time.Second*10)
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

// https://github.com/crossbario/autobahn-python/tree/master/wstest
func TestAutobahnServer(t *testing.T) {
	t.Parallel()
	if os.Getenv("AUTOBAHN") == "" {
		t.Skip("Set $AUTOBAHN to run the autobahn test suite.")
	}

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols: []string{"echo"},
		})
		if err != nil {
			t.Logf("server handshake failed: %+v", err)
			return
		}
		echoLoop(r.Context(), c)
	}))
	defer s.Close()

	spec := map[string]interface{}{
		"outdir": "ci/out/wstestServerReports",
		"servers": []interface{}{
			map[string]interface{}{
				"agent": "main",
				"url":   strings.Replace(s.URL, "http", "ws", 1),
			},
		},
		"cases": []string{"*"},
		// We skip the UTF-8 handling tests as there isn't any reason to reject invalid UTF-8, just
		// more performance overhead. 7.5.1 is the same.
		// 12.* and 13.* as we do not support compression.
		"exclude-cases": []string{"6.*", "7.5.1", "12.*", "13.*"},
	}
	specFile, err := ioutil.TempFile("", "websocketFuzzingClient.json")
	if err != nil {
		t.Fatalf("failed to create temp file for fuzzingclient.json: %v", err)
	}
	defer specFile.Close()

	e := json.NewEncoder(specFile)
	e.SetIndent("", "\t")
	err = e.Encode(spec)
	if err != nil {
		t.Fatalf("failed to write spec: %v", err)
	}

	err = specFile.Close()
	if err != nil {
		t.Fatalf("failed to close file: %v", err)
	}

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Minute*10)
	defer cancel()

	args := []string{"--mode", "fuzzingclient", "--spec", specFile.Name()}
	wstest := exec.CommandContext(ctx, "wstest", args...)
	out, err := wstest.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run wstest: %v\nout:\n%s", err, out)
	}

	checkWSTestIndex(t, "./ci/out/wstestServerReports/index.json")
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

func unusedListenAddr() (string, error) {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return "", err
	}
	l.Close()
	return l.Addr().String(), nil
}

// https://github.com/crossbario/autobahn-python/blob/master/wstest/testee_client_aio.py
func TestAutobahnClient(t *testing.T) {
	t.Parallel()
	if os.Getenv("AUTOBAHN") == "" {
		t.Skip("Set $AUTOBAHN to run the autobahn test suite.")
	}

	serverAddr, err := unusedListenAddr()
	if err != nil {
		t.Fatalf("failed to get unused listen addr for wstest: %v", err)
	}

	wsServerURL := "ws://" + serverAddr

	spec := map[string]interface{}{
		"url":    wsServerURL,
		"outdir": "ci/out/wstestClientReports",
		"cases":  []string{"*"},
		// See TestAutobahnServer for the reasons why we exclude these.
		"exclude-cases": []string{"6.*", "7.5.1", "12.*", "13.*"},
	}
	specFile, err := ioutil.TempFile("", "websocketFuzzingServer.json")
	if err != nil {
		t.Fatalf("failed to create temp file for fuzzingserver.json: %v", err)
	}
	defer specFile.Close()

	e := json.NewEncoder(specFile)
	e.SetIndent("", "\t")
	err = e.Encode(spec)
	if err != nil {
		t.Fatalf("failed to write spec: %v", err)
	}

	err = specFile.Close()
	if err != nil {
		t.Fatalf("failed to close file: %v", err)
	}

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Minute*10)
	defer cancel()

	args := []string{"--mode", "fuzzingserver", "--spec", specFile.Name(),
		// Disables some server that runs as part of fuzzingserver mode.
		// See https://github.com/crossbario/autobahn-testsuite/blob/058db3a36b7c3a1edf68c282307c6b899ca4857f/autobahntestsuite/autobahntestsuite/wstest.py#L124
		"--webport=0",
	}
	wstest := exec.CommandContext(ctx, "wstest", args...)
	err = wstest.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := wstest.Process.Kill()
		if err != nil {
			t.Error(err)
		}
	}()

	// Let it come up.
	time.Sleep(time.Second * 5)

	var cases int
	func() {
		c, _, err := websocket.Dial(ctx, wsServerURL+"/getCaseCount", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close(websocket.StatusInternalError, "")

		_, r, err := c.Reader(ctx)
		if err != nil {
			t.Fatal(err)
		}
		b, err := ioutil.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		cases, err = strconv.Atoi(string(b))
		if err != nil {
			t.Fatal(err)
		}

		c.Close(websocket.StatusNormalClosure, "")
	}()

	for i := 1; i <= cases; i++ {
		func() {
			ctx, cancel := context.WithTimeout(ctx, time.Second*45)
			defer cancel()

			c, _, err := websocket.Dial(ctx, fmt.Sprintf(wsServerURL+"/runCase?case=%v&agent=main", i), nil)
			if err != nil {
				t.Fatal(err)
			}
			echoLoop(ctx, c)
		}()
	}

	c, _, err := websocket.Dial(ctx, fmt.Sprintf(wsServerURL+"/updateReports?agent=main"), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Close(websocket.StatusNormalClosure, "")

	checkWSTestIndex(t, "./ci/out/wstestClientReports/index.json")
}

func checkWSTestIndex(t *testing.T, path string) {
	wstestOut, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read index.json: %v", err)
	}

	var indexJSON map[string]map[string]struct {
		Behavior      string `json:"behavior"`
		BehaviorClose string `json:"behaviorClose"`
	}
	err = json.Unmarshal(wstestOut, &indexJSON)
	if err != nil {
		t.Fatalf("failed to unmarshal index.json: %v", err)
	}

	var failed bool
	for _, tests := range indexJSON {
		for test, result := range tests {
			switch result.Behavior {
			case "OK", "NON-STRICT", "INFORMATIONAL":
			default:
				failed = true
				t.Errorf("test %v failed", test)
			}
			switch result.BehaviorClose {
			case "OK", "INFORMATIONAL":
			default:
				failed = true
				t.Errorf("bad close behaviour for test %v", test)
			}
		}
	}

	if failed {
		path = strings.Replace(path, ".json", ".html", 1)
		if os.Getenv("CI") == "" {
			t.Errorf("wstest found failure, please see %q (output as an artifact in CI)", path)
		}
	}
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
		return xerrors.Errorf(f+": %v", append(v, diff))
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
