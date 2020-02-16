// +build !js

package wstest

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/errd"
	"nhooyr.io/websocket/internal/test/xrand"
)

// Pipe is used to create an in memory connection
// between two websockets analogous to net.Pipe.
func Pipe(dialOpts *websocket.DialOptions, acceptOpts *websocket.AcceptOptions) (_ *websocket.Conn, _ *websocket.Conn, err error) {
	defer errd.Wrap(&err, "failed to create ws pipe")

	var serverConn *websocket.Conn
	var acceptErr error
	tt := fakeTransport{
		h: func(w http.ResponseWriter, r *http.Request) {
			serverConn, acceptErr = websocket.Accept(w, r, acceptOpts)
		},
	}

	if dialOpts == nil {
		dialOpts = &websocket.DialOptions{}
	}
	dialOpts = &*dialOpts
	dialOpts.HTTPClient = &http.Client{
		Transport: tt,
	}

	clientConn, _, err := websocket.Dial(context.Background(), "ws://example.com", dialOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to dial with fake transport: %w", err)
	}

	if serverConn == nil {
		return nil, nil, fmt.Errorf("failed to get server conn from fake transport: %w", acceptErr)
	}

	if xrand.Bool() {
		return serverConn, clientConn, nil
	}
	return clientConn, serverConn, nil
}

type fakeTransport struct {
	h http.HandlerFunc
}

func (t fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clientConn, serverConn := net.Pipe()

	hj := testHijacker{
		ResponseRecorder: httptest.NewRecorder(),
		serverConn:       serverConn,
	}

	t.h.ServeHTTP(hj, r)

	resp := hj.ResponseRecorder.Result()
	if resp.StatusCode == http.StatusSwitchingProtocols {
		resp.Body = clientConn
	}
	return resp, nil
}

type testHijacker struct {
	*httptest.ResponseRecorder
	serverConn net.Conn
}

var _ http.Hijacker = testHijacker{}

func (hj testHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return hj.serverConn, bufio.NewReadWriter(bufio.NewReader(hj.serverConn), bufio.NewWriter(hj.serverConn)), nil
}
