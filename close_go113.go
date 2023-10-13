//go:build !go1.16 && !js

package websocket

import (
	"errors"
)

var errClosed = errors.New("use of closed network connection")
