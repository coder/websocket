//go:build !js

package websocket_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/internal/test/assert"
	"github.com/coder/websocket/internal/util"
	"github.com/coder/websocket/internal/xsync"
)

func TestBadDials(t *testing.T) {
	t.Parallel()

	t.Run("badReq", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name   string
			url    string
			opts   *websocket.DialOptions
			rand   util.ReaderFunc
			nilCtx bool
		}{
			{
				name: "badURL",
				url:  "://noscheme",
			},
			{
				name: "badURLScheme",
				url:  "ftp://nhooyr.io",
			},
			{
				name: "badTLS",
				url:  "wss://totallyfake.nhooyr.io",
			},
			{
				name: "badReader",
				rand: func(p []byte) (int, error) {
					return 0, io.EOF
				},
			},
			{
				name:   "nilContext",
				url:    "http://localhost",
				nilCtx: true,
			},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				var ctx context.Context
				var cancel func()
				if !tc.nilCtx {
					ctx, cancel = context.WithTimeout(context.Background(), time.Second*5)
					defer cancel()
				}

				if tc.rand == nil {
					tc.rand = rand.Reader.Read
				}

				_, _, err := websocket.ExportedDial(ctx, tc.url, tc.opts, tc.rand)
				assert.Error(t, err)
			})
		}
	})

	t.Run("badResponse", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()

		_, _, err := websocket.Dial(ctx, "ws://example.com", &websocket.DialOptions{
			HTTPClient: mockHTTPClient(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					Body: io.NopCloser(strings.NewReader("hi")),
				}, nil
			}),
		})
		assert.Contains(t, err, "failed to WebSocket dial: expected handshake response status code 101 but got 0")
	})

	t.Run("badBody", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()

		rt := func(r *http.Request) (*http.Response, error) {
			h := http.Header{}
			h.Set("Connection", "Upgrade")
			h.Set("Upgrade", "websocket")
			h.Set("Sec-WebSocket-Accept", websocket.SecWebSocketAccept(r.Header.Get("Sec-WebSocket-Key")))

			return &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				Header:     h,
				Body:       io.NopCloser(strings.NewReader("hi")),
			}, nil
		}

		_, _, err := websocket.Dial(ctx, "ws://example.com", &websocket.DialOptions{
			HTTPClient: mockHTTPClient(rt),
		})
		assert.Contains(t, err, "response body is not a io.ReadWriteCloser")
		assert.Equal(t, "errors.Is(err, ErrBadStatus)", false, errors.Is(err, websocket.ErrBadStatus))
	})
}

func TestDialErrBadStatus(t *testing.T) {
	t.Parallel()

	t.Run("nonSwitchingStatus", func(t *testing.T) {
		t.Parallel()

		body := strings.Repeat("a", 1024) + "b"
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, body)
		}))
		defer s.Close()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()

		_, resp, err := websocket.Dial(ctx, s.URL, nil)
		assert.Error(t, err)
		assert.Equal(t, "error", "failed to WebSocket dial: expected handshake response status code 101 but got 403", err.Error())
		assert.ErrorIs(t, websocket.ErrBadStatus, err)
		// Code that unwraps to the innermost error must still get the status text.
		inner := err
		for e := errors.Unwrap(inner); e != nil; e = errors.Unwrap(inner) {
			inner = e
		}
		assert.Equal(t, "innermost error", "expected handshake response status code 101 but got 403", inner.Error())
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		assert.Equal(t, "status code", http.StatusForbidden, resp.StatusCode)
		b, err := io.ReadAll(resp.Body)
		assert.Success(t, err)
		assert.Equal(t, "body", body[:1024], string(b))
	})

	t.Run("networkError", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()

		_, resp, err := websocket.Dial(ctx, "ws://example.com", &websocket.DialOptions{
			HTTPClient: mockHTTPClient(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("connection refused")
			}),
		})
		assert.Error(t, err)
		assert.Equal(t, "errors.Is(err, ErrBadStatus)", false, errors.Is(err, websocket.ErrBadStatus))
		if resp != nil {
			t.Fatalf("expected nil response, got status %d", resp.StatusCode)
		}
	})

	redirectClient := func(checkRedirect error) *http.Client {
		hc := mockHTTPClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": {"wss://example.com/login"}},
			}, nil
		})
		hc.CheckRedirect = func(*http.Request, []*http.Request) error {
			return checkRedirect
		}
		return hc
	}

	t.Run("redirectRefused", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()

		errRefused := errors.New("redirect refused")
		_, resp, err := websocket.Dial(ctx, "ws://example.com", &websocket.DialOptions{
			HTTPClient: redirectClient(errRefused),
		})
		assert.ErrorIs(t, errRefused, err)
		assert.Equal(t, "errors.Is(err, ErrBadStatus)", false, errors.Is(err, websocket.ErrBadStatus))
		if resp != nil {
			t.Fatalf("expected nil response, got status %d", resp.StatusCode)
		}
	})

	t.Run("redirectUseLastResponse", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()

		_, resp, err := websocket.Dial(ctx, "ws://example.com", &websocket.DialOptions{
			HTTPClient: redirectClient(http.ErrUseLastResponse),
		})
		assert.ErrorIs(t, websocket.ErrBadStatus, err)
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		assert.Equal(t, "status code", http.StatusFound, resp.StatusCode)
	})
}

func Test_verifyHostOverride(t *testing.T) {
	testCases := []struct {
		name string
		host string
		exp  string
	}{
		{
			name: "noOverride",
			host: "",
			exp:  "example.com",
		},
		{
			name: "hostOverride",
			host: "example.net",
			exp:  "example.net",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			defer cancel()

			rt := func(r *http.Request) (*http.Response, error) {
				assert.Equal(t, "Host", tc.exp, r.Host)

				h := http.Header{}
				h.Set("Connection", "Upgrade")
				h.Set("Upgrade", "websocket")
				h.Set("Sec-WebSocket-Accept", websocket.SecWebSocketAccept(r.Header.Get("Sec-WebSocket-Key")))

				return &http.Response{
					StatusCode: http.StatusSwitchingProtocols,
					Header:     h,
					Body:       mockBody{bytes.NewBufferString("hi")},
				}, nil
			}

			c, _, err := websocket.Dial(ctx, "ws://example.com", &websocket.DialOptions{
				HTTPClient: mockHTTPClient(rt),
				Host:       tc.host,
			})
			assert.Success(t, err)
			c.CloseNow()
		})
	}
}

type mockBody struct {
	*bytes.Buffer
}

func (mb mockBody) Close() error {
	return nil
}

func Test_verifyServerHandshake(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		response func(w http.ResponseWriter)
		success  bool
	}{
		{
			name: "badStatus",
			response: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusOK)
			},
			success: false,
		},
		{
			name: "badConnection",
			response: func(w http.ResponseWriter) {
				w.Header().Set("Connection", "???")
				w.WriteHeader(http.StatusSwitchingProtocols)
			},
			success: false,
		},
		{
			name: "badUpgrade",
			response: func(w http.ResponseWriter) {
				w.Header().Set("Connection", "Upgrade")
				w.Header().Set("Upgrade", "???")
				w.WriteHeader(http.StatusSwitchingProtocols)
			},
			success: false,
		},
		{
			name: "badSecWebSocketAccept",
			response: func(w http.ResponseWriter) {
				w.Header().Set("Connection", "Upgrade")
				w.Header().Set("Upgrade", "websocket")
				w.Header().Set("Sec-WebSocket-Accept", "xd")
				w.WriteHeader(http.StatusSwitchingProtocols)
			},
			success: false,
		},
		{
			name: "badSecWebSocketProtocol",
			response: func(w http.ResponseWriter) {
				w.Header().Set("Connection", "Upgrade")
				w.Header().Set("Upgrade", "websocket")
				w.Header().Set("Sec-WebSocket-Protocol", "xd")
				w.WriteHeader(http.StatusSwitchingProtocols)
			},
			success: false,
		},
		{
			name: "unsupportedExtension",
			response: func(w http.ResponseWriter) {
				w.Header().Set("Connection", "Upgrade")
				w.Header().Set("Upgrade", "websocket")
				w.Header().Set("Sec-WebSocket-Extensions", "meow")
				w.WriteHeader(http.StatusSwitchingProtocols)
			},
			success: false,
		},
		{
			name: "unsupportedDeflateParam",
			response: func(w http.ResponseWriter) {
				w.Header().Set("Connection", "Upgrade")
				w.Header().Set("Upgrade", "websocket")
				w.Header().Set("Sec-WebSocket-Extensions", "permessage-deflate; meow")
				w.WriteHeader(http.StatusSwitchingProtocols)
			},
			success: false,
		},
		{
			name: "success",
			response: func(w http.ResponseWriter) {
				w.Header().Set("Connection", "Upgrade")
				w.Header().Set("Upgrade", "websocket")
				w.WriteHeader(http.StatusSwitchingProtocols)
			},
			success: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			tc.response(w)
			resp := w.Result()

			r := httptest.NewRequest("GET", "/", nil)
			key, err := websocket.SecWebSocketKey(rand.Reader)
			assert.Success(t, err)
			r.Header.Set("Sec-WebSocket-Key", key)

			if resp.Header.Get("Sec-WebSocket-Accept") == "" {
				resp.Header.Set("Sec-WebSocket-Accept", websocket.SecWebSocketAccept(key))
			}

			opts := &websocket.DialOptions{
				Subprotocols: strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ","),
			}
			_, err = websocket.VerifyServerResponse(opts, websocket.CompressionModeOpts(opts.CompressionMode), key, resp)
			if tc.success {
				assert.Success(t, err)
			} else {
				assert.Error(t, err)
			}
			assert.Equal(t, "errors.Is(err, ErrBadStatus)", tc.name == "badStatus", errors.Is(err, websocket.ErrBadStatus))
		})
	}
}

func mockHTTPClient(fn roundTripperFunc) *http.Client {
	return &http.Client{
		Transport: fn,
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestDialRedirect(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	_, _, err := websocket.Dial(ctx, "ws://example.com", &websocket.DialOptions{
		HTTPClient: mockHTTPClient(func(r *http.Request) (*http.Response, error) {
			resp := &http.Response{
				Header: http.Header{},
			}
			if r.URL.Scheme != "https" {
				resp.Header.Set("Location", "wss://example.com")
				resp.StatusCode = http.StatusFound
				return resp, nil
			}
			resp.Header.Set("Connection", "Upgrade")
			resp.Header.Set("Upgrade", "meow")
			resp.StatusCode = http.StatusSwitchingProtocols
			return resp, nil
		}),
	})
	assert.Contains(t, err, "failed to WebSocket dial: WebSocket protocol violation: Upgrade header \"meow\" does not contain websocket")
}

type forwardProxy struct {
	hc *http.Client
}

func newForwardProxy() *forwardProxy {
	return &forwardProxy{
		hc: &http.Client{},
	}
}

func (fc *forwardProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second*10)
	defer cancel()

	r = r.WithContext(ctx)
	r.RequestURI = ""
	resp, err := fc.hc.Do(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer resp.Body.Close()

	maps.Copy(w.Header(), resp.Header)
	w.Header().Set("PROXIED", "true")
	w.WriteHeader(resp.StatusCode)

	if resprw, ok := resp.Body.(io.ReadWriter); ok {
		c, brw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		brw.Flush()

		errc1 := xsync.Go(func() error {
			_, err := io.Copy(c, resprw)
			return err
		})
		errc2 := xsync.Go(func() error {
			_, err := io.Copy(resprw, c)
			return err
		})
		select {
		case <-errc1:
		case <-errc2:
		case <-r.Context().Done():
		}
	} else {
		io.Copy(w, resp.Body)
	}
}

func TestDialViaProxy(t *testing.T) {
	t.Parallel()

	ps := httptest.NewServer(newForwardProxy())
	defer ps.Close()

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := echoServer(w, r, nil)
		assert.Success(t, err)
	}))
	defer s.Close()

	psu, err := url.Parse(ps.URL)
	assert.Success(t, err)
	proxyTransport := http.DefaultTransport.(*http.Transport).Clone()
	proxyTransport.Proxy = http.ProxyURL(psu)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, s.URL, &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: proxyTransport,
		},
	})
	assert.Success(t, err)
	assert.Equal(t, "", "true", resp.Header.Get("PROXIED"))

	assertEcho(t, ctx, c)
	assertClose(t, c)
}

func TestDialFailedHandshakeBody(t *testing.T) {
	t.Parallel()

	t.Run("switchingProtocols", func(t *testing.T) {
		t.Parallel()

		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, brw, err := http.NewResponseController(w).Hijack()
			if err != nil {
				t.Errorf("failed to hijack: %v", err)
				return
			}
			defer conn.Close()
			brw.WriteString("HTTP/1.1 101 Switching Protocols\r\n" +
				"Connection: Upgrade\r\n" +
				"Upgrade: websocket\r\n" +
				"Sec-WebSocket-Accept: xd\r\n\r\n" +
				"after upgrade")
			brw.Flush()
			// Keep the connection open, so reading it would stall.
			io.Copy(io.Discard, conn)
		}))
		defer s.Close()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()

		start := time.Now()
		_, resp, err := websocket.Dial(ctx, s.URL, nil)
		elapsed := time.Since(start)
		assert.Contains(t, err, "invalid Sec-WebSocket-Accept")
		assert.Equal(t, "StatusCode", http.StatusSwitchingProtocols, resp.StatusCode)
		b, err := io.ReadAll(resp.Body)
		assert.Success(t, err)
		assert.Equal(t, "body", "", string(b))
		// Reading the body would take the 3s of Dial's read timer, and 1s
		// leaves margin for slow CI.
		if elapsed > time.Second {
			t.Fatalf("Dial took %v, want under 1s", elapsed)
		}
	})

	t.Run("forbidden", func(t *testing.T) {
		t.Parallel()

		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "forbidden by test", http.StatusForbidden)
		}))
		defer s.Close()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()

		_, resp, err := websocket.Dial(ctx, s.URL, nil)
		assert.Error(t, err)
		assert.Equal(t, "StatusCode", http.StatusForbidden, resp.StatusCode)
		b, err := io.ReadAll(resp.Body)
		assert.Success(t, err)
		assert.Equal(t, "body", "forbidden by test\n", string(b))
	})
}

func TestDialSubprotocolExactMatch(t *testing.T) {
	for _, tc := range []struct {
		name      string
		offered   []string
		selected  string
		wantError bool
	}{
		{name: "exact", offered: []string{"Chat"}, selected: "Chat"},
		{name: "case-only-mismatch", offered: []string{"Chat"}, selected: "chat", wantError: true},
		{name: "later-exact", offered: []string{"chat", "Chat"}, selected: "Chat"},
		{name: "no-selection", offered: []string{"Chat"}},
		{name: "unsolicited", selected: "Chat", wantError: true},
		{name: "not-offered", offered: []string{"Chat"}, selected: "other", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serverDone := make(chan error, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hj, ok := w.(http.Hijacker)
				if !ok {
					serverDone <- errors.New("server cannot hijack")
					return
				}
				conn, rw, err := hj.Hijack()
				if err != nil {
					serverDone <- err
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				header := http.Header{}
				header.Set("Upgrade", "websocket")
				header.Set("Connection", "Upgrade")
				header.Set("Sec-WebSocket-Accept", websocket.SecWebSocketAccept(r.Header.Get("Sec-WebSocket-Key")))
				if tc.selected != "" {
					header.Set("Sec-WebSocket-Protocol", tc.selected)
				}
				if _, err = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\n"); err == nil {
					err = header.Write(rw)
				}
				if err == nil {
					_, err = rw.WriteString("\r\n")
				}
				// A complete binary data message allows valid selections to prove
				// that the resulting connection is usable, not just non-nil.
				if err == nil {
					_, err = rw.Write([]byte{0x82, 1, 'x'})
				}
				if err == nil {
					err = rw.Flush()
				}
				if err != nil {
					serverDone <- err
					return
				}
				// Both rejected handshakes and successful CloseNow calls must
				// release the upgraded transport without waiting for a body read.
				_, err = io.Copy(io.Discard, conn)
				serverDone <- err
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c, resp, err := websocket.Dial(ctx, srv.URL, &websocket.DialOptions{Subprotocols: tc.offered})
			if c != nil {
				defer c.CloseNow()
			}
			if (err != nil) != tc.wantError {
				if c != nil {
					c.CloseNow()
				}
				t.Fatalf("Dial error = %v, wantError = %v", err, tc.wantError)
			}
			if tc.wantError {
				if c != nil || resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
					t.Fatalf("rejected handshake: conn=%v response=%v", c, resp)
				}
				if !strings.Contains(err.Error(), "unexpected Sec-WebSocket-Protocol") {
					t.Fatalf("wrong rejection: %v", err)
				}
			} else {
				if c.Subprotocol() != tc.selected {
					t.Fatalf("Subprotocol = %q, want %q", c.Subprotocol(), tc.selected)
				}
				typ, payload, err := c.Read(ctx)
				if err != nil || typ != websocket.MessageBinary || string(payload) != "x" {
					t.Fatalf("Read = %v, %q, %v", typ, payload, err)
				}
				c.CloseNow()
			}
			select {
			case err := <-serverDone:
				if err != nil {
					t.Fatalf("server transport: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("upgraded transport did not close")
			}
		})
	}
}
