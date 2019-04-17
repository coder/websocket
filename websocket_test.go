package websocket_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
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

	"github.com/google/go-cmp/cmp"
	"golang.org/x/xerrors"

	"nhooyr.io/websocket"
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
				c, err := websocket.Accept(w, r, websocket.AcceptSubprotocols("myproto"))
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")
				return nil
			},
			client: func(ctx context.Context, u string) error {
				c, resp, err := websocket.Dial(ctx, u, websocket.DialSubprotocols("myproto"))
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
				checkHeader("Sec-WebSocket-Accept", "ICX+Yqv66kxgM0FcWaLWlFLwTAI=")
				checkHeader("Sec-WebSocket-Protocol", "myproto")

				c.Close(websocket.StatusNormalClosure, "")
				return nil
			},
		},
		{
			name: "defaultSubprotocol",
			server: func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r)
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
				c, _, err := websocket.Dial(ctx, u, websocket.DialSubprotocols("meow"))
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
				c, err := websocket.Accept(w, r, websocket.AcceptSubprotocols("echo", "lar"))
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
				c, _, err := websocket.Dial(ctx, u, websocket.DialSubprotocols("poof", "echo"))
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
				c, err := websocket.Accept(w, r)
				if err == nil {
					c.Close(websocket.StatusInternalError, "")
					return xerrors.New("expected error regarding bad origin")
				}
				return nil
			},
			client: func(ctx context.Context, u string) error {
				h := http.Header{}
				h.Set("Origin", "http://unauthorized.com")
				c, _, err := websocket.Dial(ctx, u, websocket.DialHeader(h))
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
				c, err := websocket.Accept(w, r, websocket.AcceptInsecureOrigin())
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")
				return nil
			},
			client: func(ctx context.Context, u string) error {
				h := http.Header{}
				h.Set("Origin", "https://127.0.0.1")
				c, _, err := websocket.Dial(ctx, u, websocket.DialHeader(h))
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
				c, err := websocket.Accept(w, r, websocket.AcceptInsecureOrigin())
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")
				return nil
			},
			client: func(ctx context.Context, u string) error {
				h := http.Header{}
				h.Set("Origin", "https://example.com")
				c, _, err := websocket.Dial(ctx, u, websocket.DialHeader(h))
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")
				return nil
			},
		},
		{
			name: "echo",
			server: func(w http.ResponseWriter, r *http.Request) error {
				c, err := websocket.Accept(w, r)
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

				ctx, cancel := context.WithTimeout(r.Context(), time.Second*5)
				defer cancel()

				jc := websocket.JSONConn{
					Conn: c,
				}

				v := map[string]interface{}{
					"anmol": "wowow",
				}
				err = jc.Write(ctx, v)
				if err != nil {
					return err
				}

				c.Close(websocket.StatusNormalClosure, "")
				return nil
			},
			client: func(ctx context.Context, u string) error {
				c, _, err := websocket.Dial(ctx, u)
				if err != nil {
					return err
				}
				defer c.Close(websocket.StatusInternalError, "")

				jc := websocket.JSONConn{
					Conn: c,
				}

				var v interface{}
				err = jc.Read(ctx, &v)
				if err != nil {
					return err
				}

				exp := map[string]interface{}{
					"anmol": "wowow",
				}
				if !reflect.DeepEqual(exp, v) {
					return xerrors.Errorf("expected %v but got %v", exp, v)
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
				c, err := websocket.Accept(w, r)
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
				c, _, err := websocket.Dial(ctx, u,
					websocket.DialHTTPClient(hc),
				)
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
		c, err := websocket.Accept(w, r,
			websocket.AcceptSubprotocols("echo"),
		)
		if err != nil {
			t.Logf("server handshake failed: %+v", err)
			return
		}
		echoLoop(r.Context(), c)
	}))
	defer s.Close()

	spec := map[string]interface{}{
		"outdir": "wstest_reports/server",
		"servers": []interface{}{
			map[string]interface{}{
				"agent": "main",
				"url":   strings.Replace(s.URL, "http", "ws", 1),
			},
		},
		"cases":         []string{"*"},
		"exclude-cases": []string{"6.*", "7.5.1", "12.*", "13.*"},
	}
	specFile, err := ioutil.TempFile("", "websocket_fuzzingclient.json")
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

	checkWSTestIndex(t, "./wstest_reports/server/index.json")
}

func echoLoop(ctx context.Context, c *websocket.Conn) {
	defer c.Close(websocket.StatusInternalError, "")

	echo := func() error {
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()

		typ, r, err := c.Read(ctx)
		if err != nil {
			return err
		}

		w, err := c.Write(ctx, typ)
		if err != nil {
			return err
		}

		b1, _ := ioutil.ReadAll(r)
		log.Printf("%q", b1)

		_, err = io.Copy(w, r)
		if err != nil {
			return err
		}

		err = w.Close()
		if err != nil {
			return err
		}

		return nil
	}

	var i int
	for {
		err := echo()
		if err != nil {
			log.Println("WTF", err, i)
			return
		}
		i++
	}
}

// https://github.com/crossbario/autobahn-python/blob/master/wstest/testee_client_aio.py
func TestAutobahnClient(t *testing.T) {
	t.Parallel()

	spec := map[string]interface{}{
		"url":           "ws://localhost:9001",
		"outdir":        "wstest_reports/client",
		"cases":         []string{"*"},
		"exclude-cases": []string{"6.*", "7.5.1", "12.*", "13.*"},
	}
	specFile, err := ioutil.TempFile("", "websocket_fuzzingserver.json")
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

	args := []string{"--mode", "fuzzingserver", "--spec", specFile.Name()}
	if os.Getenv("CI") == "" {
		args = append([]string{"--debug"}, args...)
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
		c, _, err := websocket.Dial(ctx, "ws://localhost:9001/getCaseCount")
		if err != nil {
			t.Fatalf("failed to dial: %v", err)
		}
		defer c.Close(websocket.StatusInternalError, "")

		_, r, err := c.Read(ctx)
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

			c, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://localhost:9001/runCase?case=%v&agent=main", i))
			if err != nil {
				t.Fatalf("failed to dial: %v", err)
			}
			echoLoop(ctx, c)
		}()
	}

	c, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://localhost:9001/updateReports?agent=main"))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	c.Close(websocket.StatusNormalClosure, "")

	checkWSTestIndex(t, "./wstest_reports/client/index.json")
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
			t.Errorf("wstest found failure, please see %q", path)
		} else {
			t.Errorf("wstest found failure, please run test.sh locally to see %q", path)
		}
	}
}
