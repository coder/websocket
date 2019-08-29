package websocket

import (
	"context"
)

type Addr = websocketAddr

type Header = header

func (c *Conn) WriteFrame(ctx context.Context, fin bool, opcode opcode, p []byte) (int, error) {
	return c.writeFrame(ctx, fin, opcode, p)
}
