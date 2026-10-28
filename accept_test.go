//go:build !js

package websocket

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket/internal/test/assert"
	"github.com/coder/websocket/internal/test/xrand"
)

func TestAccept(t *testing.T) {
	t.Parallel()

	t.Run("badClientHandshake", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)

		_, err := Accept(w, r, nil)
		assert.Contains(t, err, "protocol violation")
	})

	t.Run("badOrigin", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", xrand.Base64(16))
		r.Header.Set("Origin", "harhar.com")

		_, err := Accept(w, r, nil)
		assert.Contains(t, err, `request Origin "harhar.com" is not a valid URL with a host`)
	})

	// #247
	t.Run("unauthorizedOriginErrorMessage", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", xrand.Base64(16))
		r.Header.Set("Origin", "https://harhar.com")

		_, err := Accept(w, r, nil)
		assert.Contains(t, err, `request Origin "harhar.com" is not authorized for Host "example.com"`)
	})

	t.Run("badCompression", func(t *testing.T) {
		t.Parallel()

		newRequest := func(extensions string) *http.Request {
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("Connection", "Upgrade")
			r.Header.Set("Upgrade", "websocket")
			r.Header.Set("Sec-WebSocket-Version", "13")
			r.Header.Set("Sec-WebSocket-Key", xrand.Base64(16))
			r.Header.Set("Sec-WebSocket-Extensions", extensions)
			return r
		}
		errHijack := errors.New("hijack error")
		newResponseWriter := func() http.ResponseWriter {
			return mockHijacker{
				ResponseWriter: httptest.NewRecorder(),
				hijack: func() (net.Conn, *bufio.ReadWriter, error) {
					return nil, nil, errHijack
				},
			}
		}

		t.Run("withoutFallback", func(t *testing.T) {
			t.Parallel()

			w := newResponseWriter()
			r := newRequest("permessage-deflate; harharhar")
			_, err := Accept(w, r, &AcceptOptions{
				CompressionMode: CompressionNoContextTakeover,
			})
			assert.ErrorIs(t, errHijack, err)
			assert.Equal(t, "extension header", w.Header().Get("Sec-WebSocket-Extensions"), "")
		})
		t.Run("withFallback", func(t *testing.T) {
			t.Parallel()

			w := newResponseWriter()
			r := newRequest("permessage-deflate; harharhar, permessage-deflate")
			_, err := Accept(w, r, &AcceptOptions{
				CompressionMode: CompressionNoContextTakeover,
			})
			assert.ErrorIs(t, errHijack, err)
			assert.Equal(t, "extension header",
				w.Header().Get("Sec-WebSocket-Extensions"),
				CompressionNoContextTakeover.opts().String(),
			)
		})
	})

	t.Run("requireHttpHijacker", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", xrand.Base64(16))

		_, err := Accept(w, r, nil)
		assert.Contains(t, err, `http.ResponseWriter does not implement http.Hijacker`)
	})

	t.Run("badHijack", func(t *testing.T) {
		t.Parallel()

		w := mockHijacker{
			ResponseWriter: httptest.NewRecorder(),
			hijack: func() (conn net.Conn, writer *bufio.ReadWriter, err error) {
				return nil, nil, errors.New("haha")
			},
		}

		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", xrand.Base64(16))

		_, err := Accept(w, r, nil)
		assert.Contains(t, err, `failed to hijack connection`)
	})

	t.Run("wrapperHijackerIsUnwrapped", func(t *testing.T) {
		t.Parallel()

		rr := httptest.NewRecorder()
		w := mockUnwrapper{
			ResponseWriter: rr,
			unwrap: func() http.ResponseWriter {
				return mockHijacker{
					ResponseWriter: rr,
					hijack: func() (conn net.Conn, writer *bufio.ReadWriter, err error) {
						return nil, nil, errors.New("haha")
					},
				}
			},
		}

		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", xrand.Base64(16))

		_, err := Accept(w, r, nil)
		assert.Contains(t, err, "failed to hijack connection")
	})

	t.Run("closeRace", func(t *testing.T) {
		t.Parallel()

		server, _ := net.Pipe()

		rw := bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server))
		newResponseWriter := func() http.ResponseWriter {
			return mockHijacker{
				ResponseWriter: httptest.NewRecorder(),
				hijack: func() (net.Conn, *bufio.ReadWriter, error) {
					return server, rw, nil
				},
			}
		}
		w := newResponseWriter()

		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", xrand.Base64(16))

		c, err := Accept(w, r, nil)
		wg := &sync.WaitGroup{}
		wg.Add(2)
		go func() {
			c.Close(StatusInternalError, "the sky is falling")
			wg.Done()
		}()
		go func() {
			c.CloseNow()
			wg.Done()
		}()
		wg.Wait()
		assert.Success(t, err)
	})
}

func newHandshakeRequest() *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Sec-WebSocket-Version", "13")
	r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	return r
}

// acceptRejectionTests covers every rejection before the 101 response. The
// http.ResponseWriter is an httptest.ResponseRecorder, which does not
// implement http.Hijacker.
var acceptRejectionTests = []struct {
	name       string
	modify     func(r *http.Request)
	opts       AcceptOptions
	wantStatus int
	wantErr    string
	// wantHeader is the response header when the handler runs.
	wantHeader http.Header
}{
	{
		name: "http1.0",
		modify: func(r *http.Request) {
			r.Proto, r.ProtoMajor, r.ProtoMinor = "HTTP/1.0", 1, 0
		},
		wantStatus: http.StatusUpgradeRequired,
		wantErr:    `WebSocket protocol violation: handshake request must be at least HTTP/1.1: "HTTP/1.0"`,
		wantHeader: http.Header{},
	},
	{
		name:       "noConnectionUpgrade",
		modify:     func(r *http.Request) { r.Header.Set("Connection", "keep-alive") },
		wantStatus: http.StatusUpgradeRequired,
		wantErr:    `WebSocket protocol violation: Connection header "keep-alive" does not contain Upgrade`,
		wantHeader: http.Header{"Connection": {"Upgrade"}, "Upgrade": {"websocket"}},
	},
	{
		name:       "noUpgradeWebSocket",
		modify:     func(r *http.Request) { r.Header.Set("Upgrade", "h2c") },
		wantStatus: http.StatusUpgradeRequired,
		wantErr:    `WebSocket protocol violation: Upgrade header "h2c" does not contain websocket`,
		wantHeader: http.Header{"Connection": {"Upgrade"}, "Upgrade": {"websocket"}},
	},
	{
		name:       "methodNotGet",
		modify:     func(r *http.Request) { r.Method = "POST" },
		wantStatus: http.StatusMethodNotAllowed,
		wantErr:    `WebSocket protocol violation: handshake request method is not GET but "POST"`,
		wantHeader: http.Header{},
	},
	{
		name:       "unsupportedVersion",
		modify:     func(r *http.Request) { r.Header.Set("Sec-WebSocket-Version", "8") },
		wantStatus: http.StatusBadRequest,
		wantErr:    `unsupported WebSocket protocol version (only 13 is supported): "8"`,
		wantHeader: http.Header{"Sec-Websocket-Version": {"13"}},
	},
	{
		name:       "missingKey",
		modify:     func(r *http.Request) { r.Header.Del("Sec-WebSocket-Key") },
		wantStatus: http.StatusBadRequest,
		wantErr:    "WebSocket protocol violation: missing Sec-WebSocket-Key",
		wantHeader: http.Header{},
	},
	{
		name:       "multipleKeys",
		modify:     func(r *http.Request) { r.Header.Add("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==") },
		wantStatus: http.StatusBadRequest,
		wantErr:    "WebSocket protocol violation: multiple Sec-WebSocket-Key headers",
		wantHeader: http.Header{},
	},
	{
		name:       "invalidKey",
		modify:     func(r *http.Request) { r.Header.Set("Sec-WebSocket-Key", "notbase64") },
		wantStatus: http.StatusBadRequest,
		wantErr:    `WebSocket protocol violation: invalid Sec-WebSocket-Key "notbase64", must be a 16 byte base64 encoded string`,
		wantHeader: http.Header{},
	},
	{
		name:       "unauthorizedOrigin",
		modify:     func(r *http.Request) { r.Header.Set("Origin", "https://other.com") },
		wantStatus: http.StatusForbidden,
		wantErr:    `request Origin "other.com" is not authorized for Host "example.com"`,
		wantHeader: http.Header{},
	},
	{
		// The pattern error is logged and not sent to the client.
		name:       "badOriginPattern",
		modify:     func(r *http.Request) { r.Header.Set("Origin", "https://other.com") },
		opts:       AcceptOptions{OriginPatterns: []string{"["}},
		wantStatus: http.StatusForbidden,
		wantErr:    "Forbidden",
		wantHeader: http.Header{},
	},
	{
		name:       "noHijacker",
		wantStatus: http.StatusNotImplemented,
		wantErr:    "http.ResponseWriter does not implement http.Hijacker",
		wantHeader: http.Header{},
	},
}

// TestAcceptRejectionDefault pins the response Accept writes without an
// ErrorHandler.
func TestAcceptRejectionDefault(t *testing.T) {
	t.Parallel()

	plain := func(h http.Header) http.Header {
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("X-Content-Type-Options", "nosniff")
		return h
	}
	want := map[string]struct {
		header http.Header
		body   string
	}{
		"http1.0":             {plain(http.Header{}), `WebSocket protocol violation: handshake request must be at least HTTP/1.1: "HTTP/1.0"` + "\n"},
		"noConnectionUpgrade": {plain(http.Header{"Connection": {"Upgrade"}, "Upgrade": {"websocket"}}), `WebSocket protocol violation: Connection header "keep-alive" does not contain Upgrade` + "\n"},
		"noUpgradeWebSocket":  {plain(http.Header{"Connection": {"Upgrade"}, "Upgrade": {"websocket"}}), `WebSocket protocol violation: Upgrade header "h2c" does not contain websocket` + "\n"},
		"methodNotGet":        {plain(http.Header{}), `WebSocket protocol violation: handshake request method is not GET but "POST"` + "\n"},
		"unsupportedVersion":  {plain(http.Header{"Sec-Websocket-Version": {"13"}}), `unsupported WebSocket protocol version (only 13 is supported): "8"` + "\n"},
		"missingKey":          {plain(http.Header{}), "WebSocket protocol violation: missing Sec-WebSocket-Key\n"},
		"multipleKeys":        {plain(http.Header{}), "WebSocket protocol violation: multiple Sec-WebSocket-Key headers\n"},
		"invalidKey":          {plain(http.Header{}), `WebSocket protocol violation: invalid Sec-WebSocket-Key "notbase64", must be a 16 byte base64 encoded string` + "\n"},
		"unauthorizedOrigin":  {plain(http.Header{}), `request Origin "other.com" is not authorized for Host "example.com"` + "\n"},
		"badOriginPattern":    {plain(http.Header{}), "Forbidden\n"},
		"noHijacker":          {plain(http.Header{}), "Not Implemented\n"},
	}

	for _, tc := range acceptRejectionTests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := newHandshakeRequest()
			if tc.modify != nil {
				tc.modify(r)
			}
			w := httptest.NewRecorder()

			_, err := Accept(w, r, &tc.opts)
			assert.Equal(t, "err", "failed to accept WebSocket connection: "+tc.wantErr, err.Error())
			assert.Equal(t, "status", tc.wantStatus, w.Code)
			assert.Equal(t, "header", want[tc.name].header, w.Header())
			assert.Equal(t, "body", want[tc.name].body, w.Body.String())
		})
	}
}

func Test_verifyClientHandshake(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		method  string
		http1   bool
		h       map[string]string
		success bool
	}{
		{
			name: "badConnection",
			h: map[string]string{
				"Connection": "notUpgrade",
			},
		},
		{
			name: "badUpgrade",
			h: map[string]string{
				"Connection": "Upgrade",
				"Upgrade":    "notWebSocket",
			},
		},
		{
			name:   "badMethod",
			method: "POST",
			h: map[string]string{
				"Connection": "Upgrade",
				"Upgrade":    "websocket",
			},
		},
		{
			name: "badWebSocketVersion",
			h: map[string]string{
				"Connection":            "Upgrade",
				"Upgrade":               "websocket",
				"Sec-WebSocket-Version": "14",
			},
		},
		{
			name: "missingWebSocketKey",
			h: map[string]string{
				"Connection":            "Upgrade",
				"Upgrade":               "websocket",
				"Sec-WebSocket-Version": "13",
			},
		},
		{
			name: "emptyWebSocketKey",
			h: map[string]string{
				"Connection":            "Upgrade",
				"Upgrade":               "websocket",
				"Sec-WebSocket-Version": "13",
				"Sec-WebSocket-Key":     "",
			},
		},
		{
			name: "shortWebSocketKey",
			h: map[string]string{
				"Connection":            "Upgrade",
				"Upgrade":               "websocket",
				"Sec-WebSocket-Version": "13",
				"Sec-WebSocket-Key":     xrand.Base64(15),
			},
		},
		{
			name: "invalidWebSocketKey",
			h: map[string]string{
				"Connection":            "Upgrade",
				"Upgrade":               "websocket",
				"Sec-WebSocket-Version": "13",
				"Sec-WebSocket-Key":     "notbase64",
			},
		},
		{
			name: "extraWebSocketKey",
			h: map[string]string{
				"Connection":            "Upgrade",
				"Upgrade":               "websocket",
				"Sec-WebSocket-Version": "13",
				// Kinda cheeky, but http headers are case-insensitive.
				// If 2 sec keys are present, this is a failure condition.
				"Sec-WebSocket-Key": xrand.Base64(16),
				"sec-webSocket-key": xrand.Base64(16),
			},
		},
		{
			name: "badHTTPVersion",
			h: map[string]string{
				"Connection":            "Upgrade",
				"Upgrade":               "websocket",
				"Sec-WebSocket-Version": "13",
				"Sec-WebSocket-Key":     xrand.Base64(16),
			},
			http1: true,
		},
		{
			name: "success",
			h: map[string]string{
				"Connection":            "keep-alive, Upgrade",
				"Upgrade":               "websocket",
				"Sec-WebSocket-Version": "13",
				"Sec-WebSocket-Key":     xrand.Base64(16),
			},
			success: true,
		},
		{
			name: "successSecKeyExtraSpace",
			h: map[string]string{
				"Connection":            "keep-alive, Upgrade",
				"Upgrade":               "websocket",
				"Sec-WebSocket-Version": "13",
				"Sec-WebSocket-Key":     "   " + xrand.Base64(16) + "  ",
			},
			success: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequest(tc.method, "/", nil)

			r.ProtoMajor = 1
			r.ProtoMinor = 1
			if tc.http1 {
				r.ProtoMinor = 0
			}

			for k, v := range tc.h {
				r.Header.Add(k, v)
			}

			_, err := verifyClientRequest(httptest.NewRecorder(), r)
			if tc.success {
				assert.Success(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func Test_selectSubprotocol(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		clientProtocols []string
		serverProtocols []string
		negotiated      string
	}{
		{
			name:            "empty",
			clientProtocols: nil,
			serverProtocols: nil,
			negotiated:      "",
		},
		{
			name:            "basic",
			clientProtocols: []string{"echo", "echo2"},
			serverProtocols: []string{"echo2", "echo"},
			negotiated:      "echo2",
		},
		{
			name:            "none",
			clientProtocols: []string{"echo", "echo3"},
			serverProtocols: []string{"echo2", "echo4"},
			negotiated:      "",
		},
		{
			name:            "fallback",
			clientProtocols: []string{"echo", "echo3"},
			serverProtocols: []string{"echo2", "echo3"},
			negotiated:      "echo3",
		},
		{
			name:            "clientCasePresered",
			clientProtocols: []string{"Echo1"},
			serverProtocols: []string{"echo1"},
			negotiated:      "Echo1",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("Sec-WebSocket-Protocol", strings.Join(tc.clientProtocols, ","))

			negotiated := selectSubprotocol(r, tc.serverProtocols)
			assert.Equal(t, "negotiated", tc.negotiated, negotiated)
		})
	}
}

func Test_authenticateOrigin(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		origin         string
		host           string
		originPatterns []string
		success        bool
	}{
		{
			name:    "none",
			success: true,
			host:    "example.com",
		},
		{
			name:    "invalid",
			origin:  "$#)(*)$#@*$(#@*$)#@*%)#(@*%)#(@%#@$#@$#$#@$#@}{}{}",
			host:    "example.com",
			success: false,
		},
		{
			name:    "unauthorized",
			origin:  "https://example.com",
			host:    "example1.com",
			success: false,
		},
		{
			name:    "authorized",
			origin:  "https://example.com",
			host:    "example.com",
			success: true,
		},
		{
			name:    "authorizedCaseInsensitive",
			origin:  "https://examplE.com",
			host:    "example.com",
			success: true,
		},
		{
			name:   "originPatterns",
			origin: "https://two.examplE.com",
			host:   "example.com",
			originPatterns: []string{
				"*.example.com",
				"bar.com",
			},
			success: true,
		},
		{
			name:   "originPatternsUnauthorized",
			origin: "https://two.examplE.com",
			host:   "example.com",
			originPatterns: []string{
				"exam3.com",
				"bar.com",
			},
			success: false,
		},
		{
			name:   "originPatternsWithSchemeHttps",
			origin: "https://two.example.com",
			host:   "example.com",
			originPatterns: []string{
				"https://*.example.com",
			},
			success: true,
		},
		{
			name:   "originPatternsWithSchemeMismatch",
			origin: "https://two.example.com",
			host:   "example.com",
			originPatterns: []string{
				"http://*.example.com",
			},
			success: false,
		},
		{
			name:   "originPatternsWithSchemeAndPort",
			origin: "https://example.com:8443",
			host:   "example.com",
			originPatterns: []string{
				"https://example.com:8443",
			},
			success: true,
		},
		{
			name:   "backwardsCompatHostOnlyPattern",
			origin: "http://two.example.com",
			host:   "example.com",
			originPatterns: []string{
				"*.example.com",
			},
			success: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequest("GET", "http://"+tc.host+"/", nil)
			r.Header.Set("Origin", tc.origin)

			err := authenticateOrigin(r, tc.originPatterns)
			if tc.success {
				assert.Success(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func Test_selectDeflate(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		mode     CompressionMode
		header   string
		expCopts *compressionOptions
		expOK    bool
	}{
		{
			name:     "disabled",
			mode:     CompressionDisabled,
			expCopts: nil,
			expOK:    false,
		},
		{
			name:     "noClientSupport",
			mode:     CompressionNoContextTakeover,
			expCopts: nil,
			expOK:    false,
		},
		{
			name:   "permessage-deflate",
			mode:   CompressionNoContextTakeover,
			header: "permessage-deflate; client_max_window_bits",
			expCopts: &compressionOptions{
				clientNoContextTakeover: true,
				serverNoContextTakeover: true,
			},
			expOK: true,
		},
		{
			name:   "permessage-deflate/unknown-parameter",
			mode:   CompressionNoContextTakeover,
			header: "permessage-deflate; meow",
			expOK:  false,
		},
		{
			name:   "permessage-deflate/unknown-parameter",
			mode:   CompressionNoContextTakeover,
			header: "permessage-deflate; meow, permessage-deflate; client_max_window_bits",
			expCopts: &compressionOptions{
				clientNoContextTakeover: true,
				serverNoContextTakeover: true,
			},
			expOK: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := http.Header{}
			h.Set("Sec-WebSocket-Extensions", tc.header)
			copts, ok := selectDeflate(websocketExtensions(h), tc.mode)
			assert.Equal(t, "selected options", tc.expOK, ok)
			assert.Equal(t, "compression options", tc.expCopts, copts)
		})
	}
}

type mockHijacker struct {
	http.ResponseWriter
	hijack func() (net.Conn, *bufio.ReadWriter, error)
}

var _ http.Hijacker = mockHijacker{}

func (mj mockHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return mj.hijack()
}

type mockUnwrapper struct {
	http.ResponseWriter
	unwrap func() http.ResponseWriter
}

var _ rwUnwrapper = mockUnwrapper{}

func (mu mockUnwrapper) Unwrap() http.ResponseWriter {
	return mu.unwrap()
}
