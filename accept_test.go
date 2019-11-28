package websocket

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccept(t *testing.T) {
	t.Parallel()

	t.Run("badClientHandshake", func(t *testing.T) {
		t.Parallel()

		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)

		_, err := Accept(w, r, nil)
		if err == nil {
			t.Fatalf("unexpected error value: %v", err)
		}

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
		if err == nil || !strings.Contains(err.Error(), "http.Hijacker") {
			t.Fatalf("unexpected error value: %v", err)
		}
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
				"Connection":            "Upgrade",
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

			err := verifyClientRequest(r)
			if (err == nil) != tc.success {
				t.Fatalf("unexpected error value: %+v", err)
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
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("Sec-WebSocket-Protocol", strings.Join(tc.clientProtocols, ","))

			negotiated := selectSubprotocol(r, tc.serverProtocols)
			if tc.negotiated != negotiated {
				t.Fatalf("expected %q but got %q", tc.negotiated, negotiated)
			}
		})
	}
}

func Test_authenticateOrigin(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		origin  string
		host    string
		success bool
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
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequest("GET", "http://"+tc.host+"/", nil)
			r.Header.Set("Origin", tc.origin)

			err := authenticateOrigin(r)
			if (err == nil) != tc.success {
				t.Fatalf("unexpected error value: %+v", err)
			}
		})
	}
}
