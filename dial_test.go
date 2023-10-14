//go:build !js
// +build !js

package websocket

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket/internal/test/assert"
	"nhooyr.io/websocket/internal/util"
)

func TestBadDials(t *testing.T) {
	t.Parallel()

	t.Run("badReq", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name   string
			url    string
			opts   *DialOptions
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

				_, _, err := dial(ctx, tc.url, tc.opts, tc.rand)
				assert.Error(t, err)
			})
		}
	})

	t.Run("badResponse", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()

		_, _, err := Dial(ctx, "ws://example.com", &DialOptions{
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
			h.Set("Sec-WebSocket-Accept", secWebSocketAccept(r.Header.Get("Sec-WebSocket-Key")))

			return &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				Header:     h,
				Body:       io.NopCloser(strings.NewReader("hi")),
			}, nil
		}

		_, _, err := Dial(ctx, "ws://example.com", &DialOptions{
			HTTPClient: mockHTTPClient(rt),
		})
		assert.Contains(t, err, "response body is not a io.ReadWriteCloser")
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
				h.Set("Sec-WebSocket-Accept", secWebSocketAccept(r.Header.Get("Sec-WebSocket-Key")))

				return &http.Response{
					StatusCode: http.StatusSwitchingProtocols,
					Header:     h,
					Body:       mockBody{bytes.NewBufferString("hi")},
				}, nil
			}

			_, _, err := Dial(ctx, "ws://example.com", &DialOptions{
				HTTPClient: mockHTTPClient(rt),
				Host:       tc.host,
			})
			assert.Success(t, err)
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
			key, err := secWebSocketKey(rand.Reader)
			assert.Success(t, err)
			r.Header.Set("Sec-WebSocket-Key", key)

			if resp.Header.Get("Sec-WebSocket-Accept") == "" {
				resp.Header.Set("Sec-WebSocket-Accept", secWebSocketAccept(key))
			}

			opts := &DialOptions{
				Subprotocols: strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ","),
			}
			_, err = verifyServerResponse(opts, opts.CompressionMode.opts(), key, resp)
			if tc.success {
				assert.Success(t, err)
			} else {
				assert.Error(t, err)
			}
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

	_, _, err := Dial(ctx, "ws://example.com", &DialOptions{
		HTTPClient: mockHTTPClient(func(r *http.Request) (*http.Response, error) {
			resp := &http.Response{
				Header: http.Header{},
			}
			if r.URL.Scheme != "https" {
				resp.Header.Set("Location", "wss://example.com")
				resp.StatusCode = http.StatusFound
				return resp, nil
			}
			resp.Header.Set("Connection",  "Upgrade")
			resp.Header.Set("Upgrade",  "meow")
			resp.StatusCode = http.StatusSwitchingProtocols
			return resp, nil
		}),
	})
	assert.Contains(t, err, "failed to WebSocket dial: WebSocket protocol violation: Upgrade header \"meow\" does not contain websocket")
}
