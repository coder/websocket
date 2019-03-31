package websocket

import (
	"crypto/sha1"
	"encoding/base64"
	"net/http"
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

// AcceptSubprotocols list the subprotocols that Accept will negotiate with a client.
// The first protocol that a client supports will be negotiated.
// Pass "" as a subprotocol if you would like to allow the default protocol along with
// specific subprotocols.
func AcceptSubprotocols(subprotocols ...string) AcceptOption {
	return acceptSubprotocols(subprotocols)
}

type acceptOrigins []string

func (o acceptOrigins) acceptOption() {}

// AcceptOrigins lists the origins that Accept will accept.
// Accept will always accept r.Host as the origin so you do not need to
// specify that with this option.
//
// Use this option with caution to avoid exposing your WebSocket
// server to a CSRF attack.
// See https://stackoverflow.com/a/37837709/4283659
// You can use a * for wildcards.
func AcceptOrigins(origins ...string) AcceptOption {
	return AcceptOrigins(origins...)
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

	if !httpguts.HeaderValuesContainsToken(r.Header["Connection"], "Upgrade") {
		err := xerrors.Errorf("websocket: protocol violation: Connection header does not contain Upgrade: %q", r.Header.Get("Connection"))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, err
	}

	if !httpguts.HeaderValuesContainsToken(r.Header["Upgrade"], "websocket") {
		err := xerrors.Errorf("websocket: protocol violation: Upgrade header does not contain websocket: %q", r.Header.Get("Upgrade"))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, err
	}

	if r.Method != "GET" {
		err := xerrors.Errorf("websocket: protocol violation: handshake request method is not GET: %q", r.Method)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, err
	}

	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		err := xerrors.Errorf("websocket: unsupported protocol version: %q", r.Header.Get("Sec-WebSocket-Version"))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, err
	}

	if r.Header.Get("Sec-WebSocket-Key") == "" {
		err := xerrors.New("websocket: protocol violation: missing Sec-WebSocket-Key")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, err
	}

	origins = append(origins, r.Host)

	err := authenticateOrigin(r, origins)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return nil, err
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		err = xerrors.New("websocket: response writer does not implement http.Hijacker")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, err
	}

	w.Header().Set("Upgrade", "websocket")
	w.Header().Set("Connection", "Upgrade")

	handleKey(w, r)

	selectSubprotocol(w, r, subprotocols)

	w.WriteHeader(http.StatusSwitchingProtocols)

	c, brw, err := hj.Hijack()
	if err != nil {
		err = xerrors.Errorf("websocket: failed to hijack connection: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, err
	}

	_ = c
	_ = brw

	return nil, nil
}

func selectSubprotocol(w http.ResponseWriter, r *http.Request, subprotocols []string) {
	clientSubprotocols := strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), "\n")
	for _, sp := range subprotocols {
		for _, cp := range clientSubprotocols {
			if sp == strings.TrimSpace(cp) {
				w.Header().Set("Sec-WebSocket-Protocol", sp)
				return
			}
		}
	}
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
		return xerrors.Errorf("failed to parse Origin header %q: %v", origin, err)
	}
	for _, o := range origins {
		if u.Host == o {
			return nil
		}
	}
	return xerrors.New("request origin is not authorized")
}
