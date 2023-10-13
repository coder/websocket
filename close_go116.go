//go:build go1.16
// +build go1.16

package websocket

import (
	"net"
)

var errClosed = net.ErrClosed
