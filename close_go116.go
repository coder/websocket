//go:build go1.16 && !js

package websocket

import (
	"net"
)

var errClosed = net.ErrClosed
