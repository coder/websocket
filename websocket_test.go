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
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/google/go-cmp/cmp"
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
			name: "handshake",
			server: func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
					Subprotocols: []string{"myproto"},
				})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")
				return nil
			},
			client: func(ctx context.Context, u string) error {
				c, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{
					Subprotocols: []string{"myproto"},
				})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

				checkHeader := func(h, exp string) {
					t.Helper()
					value := resp.Header.Get(h)
					if exp != value {
						t.Errorf("expected different value for header %v: %v", h, cmp.Diff(exp, value))
					}
				}

				checkHeader("Connection", "Upgrade")
				checkHeader("Upgrade", "websocket")
				checkHeader("Sec-WebSocket-Protocol", "myproto")

				c.Close(websocket.StatusNormalClosure, "")
				return nil
			},
		},
		{
			name: "defaultSubprotocol",
			server: func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r, nil)
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

				if c.Subprotocol() != "" {
					return xerrors.Errorf("unexpected subprotocol: %v", c.Subprotocol())
				}
				return nil
			},
			client: func(ctx context.Context, u string) error {
				c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
					Subprotocols: []string{"meow"},
				})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

				if c.Subprotocol() != "" {
					return xerrors.Errorf("unexpected subprotocol: %v", c.Subprotocol())
				}
				return nil
			},
		},
		{
			name: "subprotocol",
			server: func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
					Subprotocols: []string{"echo", "lar"},
				})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

				if c.Subprotocol() != "echo" {
					return xerrors.Errorf("unexpected subprotocol: %q", c.Subprotocol())
				}
				return nil
			},
			client: func(ctx context.Context, u string) error {
				c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
					Subprotocols: []string{"poof", "echo"},
				})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

				if c.Subprotocol() != "echo" {
					return xerrors.Errorf("unexpected subprotocol: %q", c.Subprotocol())
				}
				return nil
			},
		},
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
				defer c.Close(websocket.StatusInternalError, "")
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
				defer c.Close(websocket.StatusInternalError, "")
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
				defer c.Close(websocket.StatusInternalError, "")
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
				defer c.Close(websocket.StatusInternalError, "")
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
				c.Close(websocket.StatusInternalError, "")
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
				c.Close(websocket.StatusInternalError, "")
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
		name   string
		client func(ctx context.Context, c *websocket.Conn) error
		server func(ctx context.Context, c *websocket.Conn) error
	}{
		{
			name: "closeError",
			server: func(ctx context.Context, c *websocket.Conn) error {
				return wsjson.Write(ctx, c, "hello")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				var m string
				err := wsjson.Read(ctx, c, &m)
				if err != nil {
					return err
				}

				if m != "hello" {
					return xerrors.Errorf("recieved unexpected msg but expected hello: %+v", m)
				}

				_, _, err = c.Reader(ctx)
				var cerr websocket.CloseError
				if !xerrors.As(err, &cerr) || cerr.Code != websocket.StatusInternalError {
					return xerrors.Errorf("unexpected error: %+v", err)
				}

				return nil
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

				if nc.LocalAddr() != (websocket.Addr{}) {
					return xerrors.Errorf("net conn local address is not equal to websocket.Addr")
				}
				if nc.RemoteAddr() != (websocket.Addr{}) {
					return xerrors.Errorf("net conn remote address is not equal to websocket.Addr")
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
				defer nc.Close()

				nc.SetReadDeadline(time.Time{})
				time.Sleep(1)
				nc.SetReadDeadline(time.Now().Add(time.Second * 15))

				read := func() error {
					p := make([]byte, len("hello"))
					// We do not use io.ReadFull here as it masks EOFs.
					// See https://github.com/nhooyr/websocket/issues/100#issuecomment-508148024
					_, err := nc.Read(p)
					if err != nil {
						return err
					}

					if string(p) != "hello" {
						return xerrors.Errorf("unexpected payload %q received", string(p))
					}
					return nil
				}

				for i := 0; i < 3; i++ {
					err := read()
					if err != nil {
						return err
					}
				}

				// Ensure the close frame is converted to an EOF and multiple read's after all return EOF.
				err := read()
				if err != io.EOF {
					return err
				}

				err = read()
				if err != io.EOF {
					return err
				}

				return nil
			},
		},
		{
			name: "netConn/badReadMsgType",
			server: func(ctx context.Context, c *websocket.Conn) error {
				nc := websocket.NetConn(c, websocket.MessageBinary)
				defer nc.Close()

				nc.SetDeadline(time.Now().Add(time.Second * 15))

				_, err := nc.Read(make([]byte, 1))
				if err == nil || !strings.Contains(err.Error(), "unexpected frame type read") {
					return xerrors.Errorf("expected error: %+v", err)
				}

				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				err := wsjson.Write(ctx, c, "meow")
				if err != nil {
					return err
				}

				_, _, err = c.Read(ctx)
				cerr := &websocket.CloseError{}
				if !xerrors.As(err, cerr) || cerr.Code != websocket.StatusUnsupportedData {
					return xerrors.Errorf("expected close error with code StatusUnsupportedData: %+v", err)
				}

				return nil
			},
		},
		{
			name: "netConn/badRead",
			server: func(ctx context.Context, c *websocket.Conn) error {
				nc := websocket.NetConn(c, websocket.MessageBinary)
				defer nc.Close()

				nc.SetDeadline(time.Now().Add(time.Second * 15))

				_, err := nc.Read(make([]byte, 1))
				cerr := &websocket.CloseError{}
				if !xerrors.As(err, cerr) || cerr.Code != websocket.StatusBadGateway {
					return xerrors.Errorf("expected close error with code StatusBadGateway: %+v", err)
				}

				_, err = nc.Write([]byte{0xff})
				if err == nil || !strings.Contains(err.Error(), "websocket closed") {
					return xerrors.Errorf("expected writes to fail after reading a close frame: %v", err)
				}

				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				return c.Close(websocket.StatusBadGateway, "")
			},
		},
		{
			name: "jsonEcho",
			server: func(ctx context.Context, c *websocket.Conn) error {
				write := func() error {
					v := map[string]interface{}{
						"anmol": "wowow",
					}
					err := wsjson.Write(ctx, c, v)
					return err
				}
				err := write()
				if err != nil {
					return err
				}
				err = write()
				if err != nil {
					return err
				}

				c.Close(websocket.StatusNormalClosure, "")
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				read := func() error {
					var v interface{}
					err := wsjson.Read(ctx, c, &v)
					if err != nil {
						return err
					}

					exp := map[string]interface{}{
						"anmol": "wowow",
					}
					if !reflect.DeepEqual(exp, v) {
						return xerrors.Errorf("expected %v but got %v", exp, v)
					}
					return nil
				}
				err := read()
				if err != nil {
					return err
				}
				err = read()
				if err != nil {
					return err
				}

				c.Close(websocket.StatusNormalClosure, "")
				return nil
			},
		},
		{
			name: "protobufEcho",
			server: func(ctx context.Context, c *websocket.Conn) error {
				write := func() error {
					err := wspb.Write(ctx, c, ptypes.DurationProto(100))
					return err
				}
				err := write()
				if err != nil {
					return err
				}

				c.Close(websocket.StatusNormalClosure, "")
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				read := func() error {
					var v duration.Duration
					err := wspb.Read(ctx, c, &v)
					if err != nil {
						return err
					}

					d, err := ptypes.Duration(&v)
					if err != nil {
						return xerrors.Errorf("failed to convert duration.Duration to time.Duration: %w", err)
					}
					const exp = time.Duration(100)
					if !reflect.DeepEqual(exp, d) {
						return xerrors.Errorf("expected %v but got %v", exp, d)
					}
					return nil
				}
				err := read()
				if err != nil {
					return err
				}

				c.Close(websocket.StatusNormalClosure, "")
				return nil
			},
		},
		{
			name: "ping",
			server: func(ctx context.Context, c *websocket.Conn) error {
				errc := make(chan error, 1)
				go func() {
					_, _, err2 := c.Read(ctx)
					errc <- err2
				}()

				err := c.Ping(ctx)
				if err != nil {
					return err
				}

				err = c.Write(ctx, websocket.MessageText, []byte("hi"))
				if err != nil {
					return err
				}

				err = <-errc
				var ce websocket.CloseError
				if xerrors.As(err, &ce) && ce.Code == websocket.StatusNormalClosure {
					return nil
				}
				return xerrors.Errorf("unexpected error: %w", err)
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				// We read a message from the connection and then keep reading until
				// the Ping completes.
				done := make(chan struct{})
				go func() {
					_, _, err := c.Read(ctx)
					if err != nil {
						c.Close(websocket.StatusInternalError, err.Error())
						return
					}

					close(done)

					c.Read(ctx)
				}()

				err := c.Ping(ctx)
				if err != nil {
					return err
				}

				<-done

				c.Close(websocket.StatusNormalClosure, "")
				return nil
			},
		},
		{
			name: "readLimit",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				if err == nil || !strings.Contains(err.Error(), "read limited at") {
					return xerrors.Errorf("expected error but got nil: %+v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				c.CloseRead(ctx)

				err := c.Write(ctx, websocket.MessageBinary, []byte(strings.Repeat("x", 32769)))
				if err != nil {
					return err
				}

				err = c.Ping(ctx)

				var ce websocket.CloseError
				if !xerrors.As(err, &ce) || ce.Code != websocket.StatusMessageTooBig {
					return xerrors.Errorf("unexpected error: %w", err)
				}

				return nil
			},
		},
		{
			name: "wsjson/binary",
			server: func(ctx context.Context, c *websocket.Conn) error {
				var v interface{}
				err := wsjson.Read(ctx, c, &v)
				if err == nil || !strings.Contains(err.Error(), "unexpected frame type") {
					return xerrors.Errorf("expected error: %v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				return wspb.Write(ctx, c, ptypes.DurationProto(100))
			},
		},
		{
			name: "wsjson/badRead",
			server: func(ctx context.Context, c *websocket.Conn) error {
				var v interface{}
				err := wsjson.Read(ctx, c, &v)
				if err == nil || !strings.Contains(err.Error(), "failed to unmarshal json") {
					return xerrors.Errorf("expected error: %v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				return c.Write(ctx, websocket.MessageText, []byte("notjson"))
			},
		},
		{
			name: "wsjson/badWrite",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				if err == nil || !strings.Contains(err.Error(), "StatusInternalError") {
					return xerrors.Errorf("expected error: %v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				err := wsjson.Write(ctx, c, fmt.Println)
				if err == nil {
					return xerrors.Errorf("expected error: %v", err)
				}
				return nil
			},
		},
		{
			name: "wspb/text",
			server: func(ctx context.Context, c *websocket.Conn) error {
				var v proto.Message
				err := wspb.Read(ctx, c, v)
				if err == nil || !strings.Contains(err.Error(), "unexpected frame type") {
					return xerrors.Errorf("expected error: %v", err)
				}
				return nil
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
				if err == nil || !strings.Contains(err.Error(), "failed to unmarshal protobuf") {
					return xerrors.Errorf("expected error: %v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				return c.Write(ctx, websocket.MessageBinary, []byte("notpb"))
			},
		},
		{
			name: "wspb/badWrite",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				if err == nil || !strings.Contains(err.Error(), "StatusInternalError") {
					return xerrors.Errorf("expected error: %v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				err := wspb.Write(ctx, c, nil)
				if err == nil {
					return xerrors.Errorf("expected error: %v", err)
				}
				return nil
			},
		},
		{
			name: "badClose",
			server: func(ctx context.Context, c *websocket.Conn) error {
				return c.Close(9999, "")
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				cerr := &websocket.CloseError{}
				if !xerrors.As(err, cerr) || cerr.Code != websocket.StatusInternalError {
					return xerrors.Errorf("expected close error with StatusInternalError: %+v", err)
				}
				return nil
			},
		},
		{
			name: "pingTimeout",
			server: func(ctx context.Context, c *websocket.Conn) error {
				ctx, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()
				err := c.Ping(ctx)
				if err == nil || !xerrors.Is(err, context.DeadlineExceeded) {
					return xerrors.Errorf("expected nil error: %+v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				c.Read(ctx)
				return nil
			},
		},
		{
			name: "writeTimeout",
			server: func(ctx context.Context, c *websocket.Conn) error {
				c.Writer(ctx, websocket.MessageBinary)

				ctx, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()
				err := c.Write(ctx, websocket.MessageBinary, []byte("meow"))
				if !xerrors.Is(err, context.DeadlineExceeded) {
					return xerrors.Errorf("expected deadline exceeded error: %+v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				time.Sleep(time.Second)
				return nil
			},
		},
		{
			name: "readTimeout",
			server: func(ctx context.Context, c *websocket.Conn) error {
				ctx, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()
				_, _, err := c.Read(ctx)
				if !xerrors.Is(err, context.DeadlineExceeded) {
					return xerrors.Errorf("expected deadline exceeded error: %+v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				c.Read(ctx)
				return nil
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
				cerr := &websocket.CloseError{}
				if !xerrors.As(err, cerr) || cerr.Code != websocket.StatusProtocolError {
					return xerrors.Errorf("expected close error with StatusProtocolError: %+v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				if err == nil || !strings.Contains(err.Error(), "opcode") {
					return xerrors.Errorf("expected error that contains opcode: %+v", err)
				}
				return nil
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
				cerr := &websocket.CloseError{}
				if !xerrors.As(err, cerr) || cerr.Code != websocket.StatusProtocolError {
					return xerrors.Errorf("expected close error with StatusProtocolError: %+v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				if err == nil || !strings.Contains(err.Error(), "too large") {
					return xerrors.Errorf("expected error that contains too large: %+v", err)
				}
				return nil
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
				cerr := &websocket.CloseError{}
				if !xerrors.As(err, cerr) || cerr.Code != websocket.StatusProtocolError {
					return xerrors.Errorf("expected close error with StatusProtocolError: %+v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				if err == nil || !strings.Contains(err.Error(), "fragmented") {
					return xerrors.Errorf("expected error that contains fragmented: %+v", err)
				}
				return nil
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
				cerr := &websocket.CloseError{}
				if !xerrors.As(err, cerr) || cerr.Code != websocket.StatusProtocolError {
					return xerrors.Errorf("expected close error with StatusProtocolError: %+v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				if err == nil || !strings.Contains(err.Error(), "invalid status code") {
					return xerrors.Errorf("expected error that contains invalid status code: %+v", err)
				}
				return nil
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
				if err == nil || !strings.Contains(err.Error(), "previous message not read to completion") {
					return xerrors.Errorf("expected non nil error: %v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				err := c.Write(ctx, websocket.MessageBinary, []byte(strings.Repeat("x", 11)))
				if err != nil {
					return err
				}
				_, _, err = c.Read(ctx)
				if err == nil {
					return xerrors.Errorf("expected non nil error: %v", err)
				}
				return nil
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
				if err == nil || !strings.Contains(err.Error(), "previous message not read to completion") {
					return xerrors.Errorf("expected non nil error: %v", err)
				}
				return nil
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
				if err == nil {
					return xerrors.Errorf("expected non nil error: %v", err)
				}
				return nil
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
				if err == nil || !strings.Contains(err.Error(), "received new data message without finishing") {
					return xerrors.Errorf("expected non nil error: %v", err)
				}
				return nil
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
				if err == nil || !strings.Contains(err.Error(), "received new data message without finishing") {
					return xerrors.Errorf("expected non nil error: %v", err)
				}
				return nil
			},
		},
		{
			name: "continuationFrameWithoutDataFrame",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Reader(ctx)
				if err == nil || !strings.Contains(err.Error(), "received continuation frame not after data") {
					return xerrors.Errorf("expected non nil error: %v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				_, err := c.WriteFrame(ctx, false, websocket.OPContinuation, []byte(strings.Repeat("x", 10)))
				if err != nil {
					return xerrors.Errorf("expected non nil error")
				}
				return nil
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
				_, b, err := c.Read(ctx)
				if err != nil {
					return err
				}
				if string(b) != "hi" {
					return xerrors.Errorf("expected hi but got %q", string(b))
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				err := wsjson.Write(ctx, c, "hi")
				if err != nil {
					return err
				}
				return c.Write(ctx, websocket.MessageBinary, []byte("hi"))
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
				if err == nil || !strings.Contains(err.Error(), "received new data message without finishing") {
					return xerrors.Errorf("expected non nil error: %v", err)
				}
				return nil
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
				if err == nil {
					return xerrors.Errorf("expected non nil error: %v", err)
				}
				return nil
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
				if err == nil || !strings.Contains(err.Error(), "cannot use EOFed reader") {
					return xerrors.Errorf("expected non nil error: %+v", err)
				}
				return nil
			},
			client: func(ctx context.Context, c *websocket.Conn) error {
				return c.Write(ctx, websocket.MessageBinary, []byte("hi"))
			},
		},
		{
			name: "eofInPayload",
			server: func(ctx context.Context, c *websocket.Conn) error {
				_, _, err := c.Read(ctx)
				if err == nil || !strings.Contains(err.Error(), "failed to read frame payload") {
					return xerrors.Errorf("expected failed to read frame payload: %v", err)
				}
				return nil
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
				c, err := websocket.Accept(w, r, nil)
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")
				return tc.server(r.Context(), c)
			}, tls)
			defer closeFn()

			wsURL := strings.Replace(s.URL, "http", "ws", 1)

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()

			opts := &websocket.DialOptions{}
			if tls {
				opts.HTTPClient = s.Client()
			}

			c, _, err := websocket.Dial(ctx, wsURL, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close(websocket.StatusInternalError, "")

			err = tc.client(ctx, c)
			if err != nil {
				t.Fatalf("client failed: %+v", err)
			}
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

		ctx, cancel := context.WithTimeout(r.Context(), time.Second*30)
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
