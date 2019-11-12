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

	// CompressionMode sets the compression mode.
	// See docs on the CompressionMode type and defined constants.
	CompressionMode CompressionMode
}

// Accept accepts a WebSocket HTTP handshake from a client and upgrades the
// the connection to a WebSocket.
//
// Accept will reject the handshake if the Origin domain is not the same as the Host unless
// the InsecureSkipVerify option is set. In other words, by default it does not allow
// cross origin requests.
//
// If an error occurs, Accept will write a response with a safe error message to w.
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

	copts, err := acceptCompression(r, w, opts.CompressionMode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, err
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
		copts:       copts,
	}
	c.init()

	return c, nil
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
	if !strings.EqualFold(u.Host, r.Host) {
		return fmt.Errorf("request Origin %q is not authorized for Host %q", origin, r.Host)
	}
	return nil
}

func verifyClientRequest(w http.ResponseWriter, r *http.Request) error {
	if !r.ProtoAtLeast(1, 1) {
		err := fmt.Errorf("websocket protocol violation: handshake request must be at least HTTP/1.1: %q", r.Proto)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	if !headerContainsToken(r.Header, "Connection", "Upgrade") {
		err := fmt.Errorf("websocket protocol violation: Connection header %q does not contain Upgrade", r.Header.Get("Connection"))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	if !headerContainsToken(r.Header, "Upgrade", "WebSocket") {
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

func handleSecWebSocketKey(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	w.Header().Set("Sec-WebSocket-Accept", secWebSocketAccept(key))
}

func selectSubprotocol(r *http.Request, subprotocols []string) string {
	for _, sp := range subprotocols {
		if headerContainsToken(r.Header, "Sec-WebSocket-Protocol", sp) {
			return sp
		}
	}
	return ""
}

func acceptCompression(r *http.Request, w http.ResponseWriter, mode CompressionMode) (*compressionOptions, error) {
	if mode == CompressionDisabled {
		return nil, nil
	}

	for _, ext := range websocketExtensions(r.Header) {
		switch ext.name {
		case "permessage-deflate":
			return acceptDeflate(w, ext, mode)
		case "x-webkit-deflate-frame":
			return acceptWebkitDeflate(w, ext, mode)
		}
	}
	return nil, nil
}

func acceptDeflate(w http.ResponseWriter, ext websocketExtension, mode CompressionMode) (*compressionOptions, error) {
	copts := mode.opts()

	for _, p := range ext.params {
		switch p {
		case "client_no_context_takeover":
			copts.clientNoContextTakeover = true
			continue
		case "server_no_context_takeover":
			copts.serverNoContextTakeover = true
			continue
		case "client_max_window_bits", "server-max-window-bits":
			continue
		}

		return nil, fmt.Errorf("unsupported permessage-deflate parameter: %q", p)
	}

	copts.setHeader(w.Header())

	return copts, nil
}

func acceptWebkitDeflate(w http.ResponseWriter, ext websocketExtension, mode CompressionMode) (*compressionOptions, error) {
	copts := mode.opts()
	// The peer must explicitly request it.
	copts.serverNoContextTakeover = false

	for _, p := range ext.params {
		if p == "no_context_takeover" {
			copts.serverNoContextTakeover = true
			continue
		}

		// We explicitly fail on x-webkit-deflate-frame's max_window_bits parameter instead
		// of ignoring it as the draft spec is unclear. It says the server can ignore it
		// but the server has no way of signalling to the client it was ignored as the parameters
		// are set one way.
		// Thus us ignoring it would make the client think we understood it which would cause issues.
		// See https://tools.ietf.org/html/draft-tyoshino-hybi-websocket-perframe-deflate-06#section-4.1
		//
		// Either way, we're only implementing this for webkit which never sends the max_window_bits
		// parameter so we don't need to worry about it.
		return nil, fmt.Errorf("unsupported x-webkit-deflate-frame parameter: %q", p)
	}

	s := "x-webkit-deflate-frame"
	if copts.clientNoContextTakeover {
		s += "; no_context_takeover"
	}
	w.Header().Set("Sec-WebSocket-Extensions", s)

	return copts, nil
}


func headerContainsToken(h http.Header, key, token string) bool {
	token = strings.ToLower(token)

	for _, t := range headerTokens(h, key) {
		if t == token {
			return true
		}
	}
	return false
}

type websocketExtension struct {
	name   string
	params []string
}

func websocketExtensions(h http.Header) []websocketExtension {
	var exts []websocketExtension
	extStrs := headerTokens(h, "Sec-WebSocket-Extensions")
	for _, extStr := range extStrs {
		if extStr == "" {
			continue
		}

		vals := strings.Split(extStr, ";")
		for i := range vals {
			vals[i] = strings.TrimSpace(vals[i])
		}

		e := websocketExtension{
			name:   vals[0],
			params: vals[1:],
		}

		exts = append(exts, e)
	}
	return exts
}

func headerTokens(h http.Header, key string) []string {
	key = textproto.CanonicalMIMEHeaderKey(key)
	var tokens []string
	for _, v := range h[key] {
		v = strings.TrimSpace(v)
		for _, t := range strings.Split(v, ",") {
			t = strings.ToLower(t)
			tokens = append(tokens, t)
		}
	}
	return tokens
}

var keyGUID = []byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11")

func secWebSocketAccept(secWebSocketKey string) string {
	h := sha1.New()
	h.Write([]byte(secWebSocketKey))
	h.Write(keyGUID)

	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
