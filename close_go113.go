// +build !go1.16

package websocket

import (
	"errors"
)

var errClosed = errors.New("use of closed network connection")
