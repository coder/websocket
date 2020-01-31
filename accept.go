// +build !js

package websocket

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"

	"golang.org/x/xerrors"

	"nhooyr.io/websocket/internal/errd"
)

// AcceptOptions represents Accept's options.
type AcceptOptions struct {
	// Subprotocols lists the WebSocket subprotocols that Accept will negotiate with the client.
	// The empty subprotocol will always be negotiated as per RFC 6455. If you would like to
	// reject it, close the connection when c.Subprotocol() == "".
	Subprotocols []string

	// InsecureSkipVerify disables Accept's origin verification behaviour. By default,
	// the connection will only be accepted if the request origin is equal to the request
	// host.
	//
	// This is only required if you want javascript served from a different domain
	// to access your WebSocket server.
	//
	// See https://stackoverflow.com/a/37837709/4283659
	//
	// Please ensure you understand the ramifications of enabling this.
	// If used incorrectly your WebSocket server will be open to CSRF attacks.
	InsecureSkipVerify bool

	// CompressionOptions controls the compression options.
	// See docs on the CompressionOptions type.
	CompressionOptions CompressionOptions
}

// Accept accepts a WebSocket handshake from a client and upgrades the
// the connection to a WebSocket.
//
// Accept will not allow cross origin requests by default.
// See the InsecureSkipVerify option to allow cross origin requests.
//
// Accept will write a response to w on all errors.
func Accept(w http.ResponseWriter, r *http.Request, opts *AcceptOptions) (*Conn, error) {
	return accept(w, r, opts)
}

func accept(w http.ResponseWriter, r *http.Request, opts *AcceptOptions) (_ *Conn, err error) {
	defer errd.Wrap(&err, "failed to accept WebSocket connection")

	if opts == nil {
		opts = &AcceptOptions{}
	}

	err = verifyClientRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		err = xerrors.New("http.ResponseWriter does not implement http.Hijacker")
		http.Error(w, http.StatusText(http.StatusNotImplemented), http.StatusNotImplemented)
		return nil, err
	}

	w.Header().Set("Upgrade", "websocket")
	w.Header().Set("Connection", "Upgrade")

	key := r.Header.Get("Sec-WebSocket-Key")
	w.Header().Set("Sec-WebSocket-Accept", secWebSocketAccept(key))

	subproto := selectSubprotocol(r, opts.Subprotocols)
	if subproto != "" {
		w.Header().Set("Sec-WebSocket-Protocol", subproto)
	}

	copts, err := acceptCompression(r, w, opts.CompressionOptions.Mode)
	if err != nil {
		return nil, err
	}

	w.WriteHeader(http.StatusSwitchingProtocols)

	netConn, brw, err := hj.Hijack()
	if err != nil {
		err = xerrors.Errorf("failed to hijack connection: %w", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return nil, err
	}

	// https://github.com/golang/go/issues/32314
	b, _ := brw.Reader.Peek(brw.Reader.Buffered())
	brw.Reader.Reset(io.MultiReader(bytes.NewReader(b), netConn))

	return newConn(connConfig{
		subprotocol: w.Header().Get("Sec-WebSocket-Protocol"),
		rwc:         netConn,
		client:      false,
		copts:       copts,
		br:          brw.Reader,
		bw:          brw.Writer,
	}), nil
}

func verifyClientRequest(r *http.Request) error {
	if !r.ProtoAtLeast(1, 1) {
		return xerrors.Errorf("WebSocket protocol violation: handshake request must be at least HTTP/1.1: %q", r.Proto)
	}

	if !headerContainsToken(r.Header, "Connection", "Upgrade") {
		return xerrors.Errorf("WebSocket protocol violation: Connection header %q does not contain Upgrade", r.Header.Get("Connection"))
	}

	if !headerContainsToken(r.Header, "Upgrade", "websocket") {
		return xerrors.Errorf("WebSocket protocol violation: Upgrade header %q does not contain websocket", r.Header.Get("Upgrade"))
	}

	if r.Method != "GET" {
		return xerrors.Errorf("WebSocket protocol violation: handshake request method is not GET but %q", r.Method)
	}

	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		return xerrors.Errorf("unsupported WebSocket protocol version (only 13 is supported): %q", r.Header.Get("Sec-WebSocket-Version"))
	}

	if r.Header.Get("Sec-WebSocket-Key") == "" {
		return xerrors.New("WebSocket protocol violation: missing Sec-WebSocket-Key")
	}

	return nil
}

func authenticateOrigin(r *http.Request) error {
	origin := r.Header.Get("Origin")
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil {
			return xerrors.Errorf("failed to parse Origin header %q: %w", origin, err)
		}
		if !strings.EqualFold(u.Host, r.Host) {
			return xerrors.Errorf("request Origin %q is not authorized for Host %q", origin, r.Host)
		}
	}
	return nil
}

func selectSubprotocol(r *http.Request, subprotocols []string) string {
	cps := headerTokens(r.Header, "Sec-WebSocket-Protocol")
	for _, sp := range subprotocols {
		for _, cp := range cps {
			if strings.EqualFold(sp, cp) {
				return cp
			}
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
		}

		if strings.HasPrefix(p, "client_max_window_bits") || strings.HasPrefix(p, "server_max_window_bits") {
			continue
		}

		err := xerrors.Errorf("unsupported permessage-deflate parameter: %q", p)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, err
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
		err := xerrors.Errorf("unsupported x-webkit-deflate-frame parameter: %q", p)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, err
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
