package websocket

import (
	"context"
)

type Addr = websocketAddr

const OPClose = opClose
const OPBinary = opBinary
const OPPing = opPing
const OPContinuation = opContinuation

func (c *Conn) WriteFrame(ctx context.Context, fin bool, opcode opcode, p []byte) (int, error) {
	return c.writeFrame(ctx, fin, opcode, p)
}

func (c *Conn) WriteHalfFrame(ctx context.Context) (int, error) {
	return c.realWriteFrame(ctx, header{
		opcode:        opBinary,
		payloadLength: 5,
	}, make([]byte, 10))
}

func (c *Conn) Flush() error {
	return c.bw.Flush()
}
