package websocket

import (
	"crypto/sha1"
	"encoding/base64"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"

	"golang.org/x/net/http/httpguts"
	"golang.org/x/xerrors"
)

// AcceptOption is an option that can be passed to Accept.
// The implementations of this interface are printable.
type AcceptOption interface {
	acceptOption()
}

type acceptSubprotocols []string

func (o acceptSubprotocols) acceptOption() {}

// AcceptSubprotocols lists the websocket subprotocols that Accept will negotiate with a client.
// The empty subprotocol will always be negotiated as per RFC 6455. If you would like to
// reject it, close the connection if c.Subprotocol() == "".
func AcceptSubprotocols(protocols ...string) AcceptOption {
	return acceptSubprotocols(protocols)
}

type acceptOrigins []string

func (o acceptOrigins) acceptOption() {}

// AcceptOrigins lists the origins that Accept will accept.
// Accept will always accept r.Host as the origin. Use this
// option when you want to accept an origin with a different domain
// than the one the WebSocket server is running on.
//
// Use this option with caution to avoid exposing your WebSocket
// server to a CSRF attack.
// See https://stackoverflow.com/a/37837709/4283659
func AcceptOrigins(origins ...string) AcceptOption {
	return acceptOrigins(origins)
}

func verifyClientRequest(w http.ResponseWriter, r *http.Request) error {
	if !headerValuesContainsToken(r.Header, "Connection", "Upgrade") {
		err := xerrors.Errorf("websocket: protocol violation: Connection header does not contain Upgrade: %q", r.Header.Get("Connection"))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	if !headerValuesContainsToken(r.Header, "Upgrade", "WebSocket") {
		err := xerrors.Errorf("websocket: protocol violation: Upgrade header does not contain websocket: %q", r.Header.Get("Upgrade"))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	if r.Method != "GET" {
		err := xerrors.Errorf("websocket: protocol violation: handshake request method is not GET: %q", r.Method)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		err := xerrors.Errorf("websocket: unsupported protocol version: %q", r.Header.Get("Sec-WebSocket-Version"))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	if r.Header.Get("Sec-WebSocket-Key") == "" {
		err := xerrors.New("websocket: protocol violation: missing Sec-WebSocket-Key")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	return nil
}

// Accept accepts a WebSocket handshake from a client and upgrades the
// the connection to WebSocket.
// Accept will reject the handshake if the Origin is not the same as the Host unless
// InsecureAcceptOrigin is passed.
// Accept uses w to write the handshake response so the timeouts on the http.Server apply.
func Accept(w http.ResponseWriter, r *http.Request, opts ...AcceptOption) (*Conn, error) {
	var subprotocols []string
	origins := []string{r.Host}
	for _, opt := range opts {
		switch opt := opt.(type) {
		case acceptOrigins:
			origins = []string(opt)
		case acceptSubprotocols:
			subprotocols = []string(opt)
		}
	}

	err := verifyClientRequest(w, r)
	if err != nil {
		return nil, err
	}

	origins = append(origins, r.Host)

	err = authenticateOrigin(r, origins)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return nil, err
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		err = xerrors.New("websocket: response writer does not implement http.Hijacker")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return nil, err
	}

	w.Header().Set("Upgrade", "websocket")
	w.Header().Set("Connection", "Upgrade")

	handleKey(w, r)

	subproto := selectSubprotocol(r, subprotocols)
	if subproto != "" {
		w.Header().Set("Sec-WebSocket-Protocol", subproto)
	}

	w.WriteHeader(http.StatusSwitchingProtocols)

	netConn, brw, err := hj.Hijack()
	if err != nil {
		err = xerrors.Errorf("websocket: failed to hijack connection: %w", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return nil, err
	}

	c := &Conn{
		subprotocol: w.Header().Get("Sec-WebSocket-Protocol"),
		br:          brw.Reader,
		bw:          brw.Writer,
		closer:      netConn,
	}
	c.init()

	return c, nil
}

func headerValuesContainsToken(h http.Header, key, val string) bool {
	key = textproto.CanonicalMIMEHeaderKey(key)
	return httpguts.HeaderValuesContainsToken(h[key], val)
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

func handleKey(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	h := sha1.New()
	h.Write([]byte(key))
	h.Write(keyGUID)

	responseKey := base64.StdEncoding.EncodeToString(h.Sum(nil))
	w.Header().Set("Sec-WebSocket-Accept", responseKey)
}

func authenticateOrigin(r *http.Request, origins []string) error {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil {
		return xerrors.Errorf("failed to parse Origin header %q: %w", origin, err)
	}
	for _, o := range origins {
		if strings.EqualFold(u.Host, o) {
			return nil
		}
	}
	return xerrors.Errorf("request origin %q is not authorized", r.Header.Get("Origin"))
}
