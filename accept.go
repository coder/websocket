package websocket

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
)

// AcceptOptions represents the options available to pass to Accept.
type AcceptOptions struct {
	// Subprotocols lists the websocket subprotocols that Accept will negotiate with a client.
	// The empty subprotocol will always be negotiated as per RFC 6455. If you would like to
	// reject it, close the connection if c.Subprotocol() == "".
	Subprotocols []string

	// InsecureSkipVerify disables Accept's origin verification
	// behaviour. By default Accept only allows the handshake to
	// succeed if the javascript that is initiating the handshake
	// is on the same domain as the server. This is to prevent CSRF
	// attacks when secure data is stored in a cookie as there is no same
	// origin policy for WebSockets. In other words, javascript from
	// any domain can perform a WebSocket dial on an arbitrary server.
	// This dial will include cookies which means the arbitrary javascript
	// can perform actions as the authenticated user.
	//
	// See https://stackoverflow.com/a/37837709/4283659
	//
	// The only time you need this is if your javascript is running on a different domain
	// than your WebSocket server.
	// Think carefully about whether you really need this option before you use it.
	// If you do, remember that if you store secure data in cookies, you wil need to verify the
	// Origin header yourself otherwise you are exposing yourself to a CSRF attack.
	InsecureSkipVerify bool
}

func verifyClientRequest(w http.ResponseWriter, r *http.Request) error {
	if !headerValuesContainsToken(r.Header, "Connection", "Upgrade") {
		err := fmt.Errorf("websocket protocol violation: Connection header %q does not contain Upgrade", r.Header.Get("Connection"))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	if !headerValuesContainsToken(r.Header, "Upgrade", "WebSocket") {
		err := fmt.Errorf("websocket protocol violation: Upgrade header %q does not contain websocket", r.Header.Get("Upgrade"))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	if r.Method != "GET" {
		err := fmt.Errorf("websocket protocol violation: handshake request method is not GET but %q", r.Method)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		err := fmt.Errorf("unsupported websocket protocol version (only 13 is supported): %q", r.Header.Get("Sec-WebSocket-Version"))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	if r.Header.Get("Sec-WebSocket-Key") == "" {
		err := errors.New("websocket protocol violation: missing Sec-WebSocket-Key")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	return nil
}

// Accept accepts a WebSocket handshake from a client and upgrades the
// the connection to a WebSocket.
//
// Accept will reject the handshake if the Origin domain is not the same as the Host unless
// the InsecureSkipVerify option is set. In other words, by default it does not allow
// cross origin requests.
//
// If an error occurs, Accept will always write an appropriate response so you do not
// have to.
func Accept(w http.ResponseWriter, r *http.Request, opts *AcceptOptions) (*Conn, error) {
	c, err := accept(w, r, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to accept websocket connection: %w", err)
	}
	return c, nil
}

func accept(w http.ResponseWriter, r *http.Request, opts *AcceptOptions) (*Conn, error) {
	if opts == nil {
		opts = &AcceptOptions{}
	}

	err := verifyClientRequest(w, r)
	if err != nil {
		return nil, err
	}

	if !opts.InsecureSkipVerify {
		err = authenticateOrigin(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return nil, err
		}
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		err = errors.New("passed ResponseWriter does not implement http.Hijacker")
		http.Error(w, http.StatusText(http.StatusNotImplemented), http.StatusNotImplemented)
		return nil, err
	}

	w.Header().Set("Upgrade", "websocket")
	w.Header().Set("Connection", "Upgrade")

	handleSecWebSocketKey(w, r)

	subproto := selectSubprotocol(r, opts.Subprotocols)
	if subproto != "" {
		w.Header().Set("Sec-WebSocket-Protocol", subproto)
	}

	w.WriteHeader(http.StatusSwitchingProtocols)

	netConn, brw, err := hj.Hijack()
	if err != nil {
		err = fmt.Errorf("failed to hijack connection: %w", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return nil, err
	}

	// https://github.com/golang/go/issues/32314
	b, _ := brw.Reader.Peek(brw.Reader.Buffered())
	brw.Reader.Reset(io.MultiReader(bytes.NewReader(b), netConn))

	c := &Conn{
		subprotocol: w.Header().Get("Sec-WebSocket-Protocol"),
		br:          brw.Reader,
		bw:          brw.Writer,
		closer:      netConn,
	}
	c.init()

	return c, nil
}

func headerValuesContainsToken(h http.Header, key, token string) bool {
	key = textproto.CanonicalMIMEHeaderKey(key)

	for _, val2 := range h[key] {
		if headerValueContainsToken(val2, token) {
			return true
		}
	}

	return false
}

func headerValueContainsToken(val2, token string) bool {
	val2 = strings.TrimSpace(val2)

	for _, val2 := range strings.Split(val2, ",") {
		val2 = strings.TrimSpace(val2)
		if strings.EqualFold(val2, token) {
			return true
		}
	}

	return false
}

func selectSubprotocol(r *http.Request, subprotocols []string) string {
	for _, sp := range subprotocols {
		if headerValuesContainsToken(r.Header, "Sec-WebSocket-Protocol", sp) {
			return sp
		}
	}
	return ""
}

var keyGUID = []byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11")

func handleSecWebSocketKey(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	w.Header().Set("Sec-WebSocket-Accept", secWebSocketAccept(key))
}

func secWebSocketAccept(secWebSocketKey string) string {
	h := sha1.New()
	h.Write([]byte(secWebSocketKey))
	h.Write(keyGUID)

	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func authenticateOrigin(r *http.Request) error {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("failed to parse Origin header %q: %w", origin, err)
	}
	if strings.EqualFold(u.Host, r.Host) {
		return nil
	}
	return fmt.Errorf("request Origin %q is not authorized for Host %q", origin, r.Host)
}
