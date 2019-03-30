package websocket

import (
	"fmt"
	"net/http"
)

// AcceptOption is an option that can be passed to Accept.
type AcceptOption interface {
	acceptOption()
	fmt.Stringer
}

// AcceptSubprotocols list the subprotocols that Accept will negotiate with a client.
// The first protocol that a client supports will be negotiated.
// Pass "" as a subprotocol if you would like to allow the default protocol.
func AcceptSubprotocols(subprotocols ...string) AcceptOption {
	panic("TODO")
}

// AcceptOrigins lists the origins that Accept will accept.
// Accept will always accept r.Host as the origin so you do not need to
// specify that with this option.
//
// Use this option with caution to avoid exposing your WebSocket
// server to a CSRF attack.
// See https://stackoverflow.com/a/37837709/4283659
// You can use a * to specify wildcards.
func AcceptOrigins(origins ...string) AcceptOption {
	panic("TODO")
}

// Accept accepts a WebSocket handshake from a client and upgrades the
// the connection to WebSocket.
// Accept will reject the handshake if the Origin is not the same as the Host unless
// InsecureAcceptOrigin is passed.
// Accept uses w to write the handshake response so the timeouts on the http.Server apply.
func Accept(w http.ResponseWriter, r *http.Request, opts ...AcceptOption) (*Conn, error) {
	panic("TODO")
}
