// +build js

package websocket

import (
	"bytes"
	"context"
	"io"
)

func (c *netConn) netConnReader(ctx context.Context) (MessageType, io.Reader, error) {
	typ, p, err := c.c.Read(ctx)
	if err != nil {
		return 0, nil, err
	}
	return typ, bytes.NewReader(p), nil
}
