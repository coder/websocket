//go:build !js
// +build !js

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

	"nhooyr.io/websocket/internal/test/assert"
	"nhooyr.io/websocket/internal/test/xrand"
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
