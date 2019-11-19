// +build !js

package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBadDials(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		url  string
		opts *DialOptions
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
			name: "badHTTPClient",
			url:  "ws://nhooyr.io",
			opts: &DialOptions{
				HTTPClient: &http.Client{
					Timeout: time.Minute,
				},
			},
		},
		{
			name: "badTLS",
			url:  "wss://totallyfake.nhooyr.io",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			defer cancel()

			_, _, err := Dial(ctx, tc.url, tc.opts)
			if err == nil {
				t.Fatalf("expected non nil error: %+v", err)
			}
		})
	}
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
			key, err := secWebSocketKey()
			if err != nil {
				t.Fatal(err)
			}
			r.Header.Set("Sec-WebSocket-Key", key)

			if resp.Header.Get("Sec-WebSocket-Accept") == "" {
				resp.Header.Set("Sec-WebSocket-Accept", secWebSocketAccept(key))
			}

			_, err = verifyServerResponse(r, resp)
			if (err == nil) != tc.success {
				t.Fatalf("unexpected error: %+v", err)
			}
		})
	}
}
