package websocket

import (
	"net/http"

	"golang.org/x/xerrors"
)

// AcceptOptions represents Accept's options.
type AcceptOptions struct {
	Subprotocols       []string
	InsecureSkipVerify bool
	CompressionOptions *CompressionOptions
}

// Accept is stubbed out for Wasm.
func Accept(w http.ResponseWriter, r *http.Request, opts *AcceptOptions) (*Conn, error) {
	return nil, xerrors.New("unimplemented")
}
