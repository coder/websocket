// +build !js

package websocket

import (
	"context"
	"io"
)

func (c *netConn) netConnReader(ctx context.Context) (MessageType, io.Reader, error) {
	return c.c.Reader(c.readContext)
}
