package websocket

import (
	"bufio"
	"context"

	"golang.org/x/xerrors"
)

type (
	Addr   = websocketAddr
	OpCode int
)

const (
	OpClose        = OpCode(opClose)
	OpBinary       = OpCode(opBinary)
	OpPing         = OpCode(opPing)
	OpPong         = OpCode(opPong)
	OpContinuation = OpCode(opContinuation)
)

func (c *Conn) ReadFrame(ctx context.Context) (OpCode, []byte, error) {
	h, err := c.readFrameHeader(ctx)
	if err != nil {
		return 0, nil, err
	}
	b := make([]byte, h.payloadLength)
	_, err = c.readFramePayload(ctx, b)
	if err != nil {
		return 0, nil, err
	}
	if h.masked {
		fastXOR(h.maskKey, 0, b)
	}
	return OpCode(h.opcode), b, nil
}

func (c *Conn) WriteFrame(ctx context.Context, fin bool, opc OpCode, p []byte) (int, error) {
	return c.writeFrame(ctx, fin, opcode(opc), p)
}

func (c *Conn) WriteHeader(ctx context.Context, fin bool, opc OpCode, lenp int64) error {
	headerBytes := writeHeader(c.writeHeaderBuf, header{
		fin:           fin,
		opcode:        opcode(opc),
		payloadLength: lenp,
		masked:        c.client,
	})
	_, err := c.bw.Write(headerBytes)
	if err != nil {
		return xerrors.Errorf("failed to write header: %w", err)
	}
	return nil
}

func (c *Conn) PingWithPayload(ctx context.Context, p string) error {
	return c.ping(ctx, p)
}

func (c *Conn) WriteHalfFrame(ctx context.Context) (int, error) {
	return c.realWriteFrame(ctx, header{
		fin:           true,
		opcode:        opBinary,
		payloadLength: 10,
	}, make([]byte, 5))
}

func (c *Conn) CloseUnderlyingConn() {
	c.closer.Close()
}

func (c *Conn) Flush() error {
	return c.bw.Flush()
}

func (c CloseError) Bytes() ([]byte, error) {
	return c.bytes()
}

func (c *Conn) BW() *bufio.Writer {
	return c.bw
}

func (c *Conn) WriteClose(ctx context.Context, code StatusCode, reason string) ([]byte, error) {
	b, err := CloseError{
		Code:   code,
		Reason: reason,
	}.Bytes()
	if err != nil {
		return nil, err
	}
	_, err = c.WriteFrame(ctx, true, OpClose, b)
	if err != nil {
		return nil, err
	}
	return b, nil
}
