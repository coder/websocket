// +build !js

package websocket

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nhooyr.io/websocket/internal/test/assert"
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
		r.Header.Set("Sec-WebSocket-Key", "meow123")
		r.Header.Set("Origin", "harhar.com")

		_, err := Accept(w, r, nil)
		assert.Contains(t, err, `request Origin "harhar.com" is not authorized for Host`)
	})

	t.Run("badCompression", func(t *testing.T) {
		t.Parallel()

		w := mockHijacker{
			ResponseWriter: httptest.NewRecorder(),
		}
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", "meow123")
		r.Header.Set("Sec-WebSocket-Extensions", "permessage-deflate; harharhar")

		_, err := Accept(w, r, nil)
		assert.Contains(t, err, `unsupported permessage-deflate parameter`)
	})

	t.Run("requireHttpHijacker", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", "meow123")

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
		r.Header.Set("Sec-WebSocket-Key", "meow123")

		_, err := Accept(w, r, nil)
		assert.Contains(t, err, `failed to hijack connection`)
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
			name: "badWebSocketKey",
			h: map[string]string{
				"Connection":            "Upgrade",
				"Upgrade":               "websocket",
				"Sec-WebSocket-Version": "13",
				"Sec-WebSocket-Key":     "",
			},
		},
		{
			name: "badHTTPVersion",
			h: map[string]string{
				"Connection":            "Upgrade",
				"Upgrade":               "websocket",
				"Sec-WebSocket-Version": "13",
				"Sec-WebSocket-Key":     "meow123",
			},
			http1: true,
		},
		{
			name: "success",
			h: map[string]string{
				"Connection":            "keep-alive, Upgrade",
				"Upgrade":               "websocket",
				"Sec-WebSocket-Version": "13",
				"Sec-WebSocket-Key":     "meow123",
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
				r.Header.Set(k, v)
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

func Test_acceptCompression(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                       string
		mode                       CompressionMode
		reqSecWebSocketExtensions  string
		respSecWebSocketExtensions string
		expCopts                   *compressionOptions
		error                      bool
	}{
		{
			name:     "disabled",
			mode:     CompressionDisabled,
			expCopts: nil,
		},
		{
			name:     "noClientSupport",
			mode:     CompressionNoContextTakeover,
			expCopts: nil,
		},
		{
			name:                       "permessage-deflate",
			mode:                       CompressionNoContextTakeover,
			reqSecWebSocketExtensions:  "permessage-deflate; client_max_window_bits",
			respSecWebSocketExtensions: "permessage-deflate; client_no_context_takeover; server_no_context_takeover",
			expCopts: &compressionOptions{
				clientNoContextTakeover: true,
				serverNoContextTakeover: true,
			},
		},
		{
			name:                      "permessage-deflate/error",
			mode:                      CompressionNoContextTakeover,
			reqSecWebSocketExtensions: "permessage-deflate; meow",
			error:                     true,
		},
		// {
		// 	name:                       "x-webkit-deflate-frame",
		// 	mode:                       CompressionNoContextTakeover,
		// 	reqSecWebSocketExtensions:  "x-webkit-deflate-frame; no_context_takeover",
		// 	respSecWebSocketExtensions: "x-webkit-deflate-frame; no_context_takeover",
		// 	expCopts: &compressionOptions{
		// 		clientNoContextTakeover: true,
		// 		serverNoContextTakeover: true,
		// 	},
		// },
		// {
		// 	name:                      "x-webkit-deflate/error",
		// 	mode:                      CompressionNoContextTakeover,
		// 	reqSecWebSocketExtensions: "x-webkit-deflate-frame; max_window_bits",
		// 	error:                     true,
		// },
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("Sec-WebSocket-Extensions", tc.reqSecWebSocketExtensions)

			w := httptest.NewRecorder()
			copts, err := acceptCompression(r, w, tc.mode)
			if tc.error {
				assert.Error(t, err)
				return
			}

			assert.Success(t, err)
			assert.Equal(t, "compression options", tc.expCopts, copts)
			assert.Equal(t, "Sec-WebSocket-Extensions", tc.respSecWebSocketExtensions, w.Header().Get("Sec-WebSocket-Extensions"))
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
