package websocket_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
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

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"
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
				c, err := websocket.Accept(w, r, websocket.AcceptOptions{
					Subprotocols: []string{"myproto"},
				})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")
				return nil
			},
			client: func(ctx context.Context, u string) error {
				c, resp, err := websocket.Dial(ctx, u, websocket.DialOptions{
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
			name: "closeError",
			server: func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r, websocket.AcceptOptions{})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

				err = wsjson.Write(r.Context(), c, "hello")
				if err != nil {
					return err
				}

				return nil
			},
			client: func(ctx context.Context, u string) error {
				c, _, err := websocket.Dial(ctx, u, websocket.DialOptions{
					Subprotocols: []string{"meow"},
				})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

				var m string
				err = wsjson.Read(ctx, c, &m)
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
			server: func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r, websocket.AcceptOptions{})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

				nc := websocket.NetConn(c, websocket.MessageBinary)
				defer nc.Close()

				nc.SetWriteDeadline(time.Time{})
				time.Sleep(1)
				nc.SetWriteDeadline(time.Now().Add(time.Second * 15))

				for i := 0; i < 3; i++ {
					_, err = nc.Write([]byte("hello"))
					if err != nil {
						return err
					}
				}

				return nil
			},
			client: func(ctx context.Context, u string) error {
				c, _, err := websocket.Dial(ctx, u, websocket.DialOptions{
					Subprotocols: []string{"meow"},
				})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

				nc := websocket.NetConn(c, websocket.MessageBinary)
				defer nc.Close()

				nc.SetReadDeadline(time.Time{})
				time.Sleep(1)
				nc.SetReadDeadline(time.Now().Add(time.Second * 15))

				read := func() error {
					p := make([]byte, len("hello"))
					// We do not use io.ReadFull here as it masks EOFs.
					// See https://github.com/nhooyr/websocket/issues/100#issuecomment-508148024
					_, err = nc.Read(p)
					if err != nil {
						return err
					}

					if string(p) != "hello" {
						return xerrors.Errorf("unexpected payload %q received", string(p))
					}
					return nil
				}

				for i := 0; i < 3; i++ {
					err = read()
					if err != nil {
						return err
					}
				}

				// Ensure the close frame is converted to an EOF and multiple read's after all return EOF.
				err = read()
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
			name: "defaultSubprotocol",
			server: func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r, websocket.AcceptOptions{})
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
				c, _, err := websocket.Dial(ctx, u, websocket.DialOptions{
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
				c, err := websocket.Accept(w, r, websocket.AcceptOptions{
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
				c, _, err := websocket.Dial(ctx, u, websocket.DialOptions{
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
				c, err := websocket.Accept(w, r, websocket.AcceptOptions{})
				if err == nil {
					c.Close(websocket.StatusInternalError, "")
					return xerrors.New("expected error regarding bad origin")
				}
				return nil
			},
			client: func(ctx context.Context, u string) error {
				h := http.Header{}
				h.Set("Origin", "http://unauthorized.com")
				c, _, err := websocket.Dial(ctx, u, websocket.DialOptions{
					HTTPHeader: h,
				})
				if err == nil {
					c.Close(websocket.StatusInternalError, "")
					return xerrors.New("expected handshake failure")
				}
				return nil
			},
		},
		{
			name: "acceptSecureOrigin",
			server: func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r, websocket.AcceptOptions{})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")
				return nil
			},
			client: func(ctx context.Context, u string) error {
				h := http.Header{}
				h.Set("Origin", u)
				c, _, err := websocket.Dial(ctx, u, websocket.DialOptions{
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
				c, err := websocket.Accept(w, r, websocket.AcceptOptions{
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
				c, _, err := websocket.Dial(ctx, u, websocket.DialOptions{
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
			name: "jsonEcho",
			server: func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r, websocket.AcceptOptions{})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

				ctx, cancel := context.WithTimeout(r.Context(), time.Second*5)
				defer cancel()

				write := func() error {
					v := map[string]interface{}{
						"anmol": "wowow",
					}
					err := wsjson.Write(ctx, c, v)
					return err
				}
				err = write()
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
			client: func(ctx context.Context, u string) error {
				c, _, err := websocket.Dial(ctx, u, websocket.DialOptions{})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

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
				err = read()
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
			server: func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r, websocket.AcceptOptions{})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

				ctx, cancel := context.WithTimeout(r.Context(), time.Second*5)
				defer cancel()

				write := func() error {
					err := wspb.Write(ctx, c, ptypes.DurationProto(100))
					return err
				}
				err = write()
				if err != nil {
					return err
				}

				c.Close(websocket.StatusNormalClosure, "")
				return nil
			},
			client: func(ctx context.Context, u string) error {
				c, _, err := websocket.Dial(ctx, u, websocket.DialOptions{})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

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
				err = read()
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
				c, err := websocket.Accept(w, r, websocket.AcceptOptions{})
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
				c, _, err := websocket.Dial(ctx, u, websocket.DialOptions{
					HTTPClient: hc,
				})
				if err != nil {
					return err
				}
				c.Close(websocket.StatusInternalError, "")
				return nil
			},
		},
		{
			name: "ping",
			server: func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r, websocket.AcceptOptions{})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

				errc := make(chan error, 1)
				go func() {
					_, _, err2 := c.Read(r.Context())
					errc <- err2
				}()

				err = c.Ping(r.Context())
				if err != nil {
					return err
				}

				err = c.Write(r.Context(), websocket.MessageText, []byte("hi"))
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
			client: func(ctx context.Context, u string) error {
				c, _, err := websocket.Dial(ctx, u, websocket.DialOptions{})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

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

				err = c.Ping(ctx)
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
			server: func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r, websocket.AcceptOptions{})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

				_, _, err = c.Read(r.Context())
				if err == nil {
					return xerrors.Errorf("expected error but got nil")
				}
				return nil
			},
			client: func(ctx context.Context, u string) error {
				c, _, err := websocket.Dial(ctx, u, websocket.DialOptions{})
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

				go c.Reader(ctx)

				err = c.Write(ctx, websocket.MessageBinary, []byte(strings.Repeat("x", 32769)))
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
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s, closeFn := testServer(t, func(w http.ResponseWriter, r *http.Request) {
				err := tc.server(w, r)
				if err != nil {
					t.Errorf("server failed: %+v", err)
					return
				}
			})
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

func testServer(tb testing.TB, fn http.HandlerFunc) (s *httptest.Server, closeFn func()) {
	var conns int64
	s = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&conns, 1)
		defer atomic.AddInt64(&conns, -1)

		fn.ServeHTTP(w, r)
	}))
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

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, websocket.AcceptOptions{
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
		c, _, err := websocket.Dial(ctx, wsServerURL+"/getCaseCount", websocket.DialOptions{})
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

			c, _, err := websocket.Dial(ctx, fmt.Sprintf(wsServerURL+"/runCase?case=%v&agent=main", i), websocket.DialOptions{})
			if err != nil {
				t.Fatal(err)
			}
			echoLoop(ctx, c)
		}()
	}

	c, _, err := websocket.Dial(ctx, fmt.Sprintf(wsServerURL+"/updateReports?agent=main"), websocket.DialOptions{})
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
	s, closeFn := testServer(b, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, websocket.AcceptOptions{})
		if err != nil {
			b.Logf("server handshake failed: %+v", err)
			return
		}
		if echo {
			echoLoop(r.Context(), c)
		} else {
			discardLoop(r.Context(), c)
		}
	}))
	defer closeFn()

	wsURL := strings.Replace(s.URL, "http", "ws", 1)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL, websocket.DialOptions{})
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
				benchConn(b, false, false, size)
			})
		}
	})
}
