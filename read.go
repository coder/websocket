// +build !js

package websocket

import (
	"context"
	"io"
	"io/ioutil"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/xerrors"

	"nhooyr.io/websocket/internal/errd"
)

// Reader reads from the connection until until there is a WebSocket
// data message to be read. It will handle ping, pong and close frames as appropriate.
//
// It returns the type of the message and an io.Reader to read it.
// The passed context will also bound the reader.
// Ensure you read to EOF otherwise the connection will hang.
//
// Call CloseRead if you do not expect any data messages from the peer.
//
// Only one Reader may be open at a time.
func (c *Conn) Reader(ctx context.Context) (MessageType, io.Reader, error) {
	return c.reader(ctx)
}

// Read is a convenience method around Reader to read a single message
// from the connection.
func (c *Conn) Read(ctx context.Context) (MessageType, []byte, error) {
	typ, r, err := c.Reader(ctx)
	if err != nil {
		return 0, nil, err
	}

	b, err := ioutil.ReadAll(r)
	return typ, b, err
}

// CloseRead starts a goroutine to read from the connection until it is closed
// or a data message is received.
//
// Once CloseRead is called you cannot read any messages from the connection.
// The returned context will be cancelled when the connection is closed.
//
// If a data message is received, the connection will be closed with StatusPolicyViolation.
//
// Call CloseRead when you do not expect to read any more messages.
// Since it actively reads from the connection, it will ensure that ping, pong and close
// frames are responded to.
func (c *Conn) CloseRead(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer cancel()
		c.Reader(ctx)
		c.Close(StatusPolicyViolation, "unexpected data message")
	}()
	return ctx
}

// SetReadLimit sets the max number of bytes to read for a single message.
// It applies to the Reader and Read methods.
//
// By default, the connection has a message read limit of 32768 bytes.
//
// When the limit is hit, the connection will be closed with StatusMessageTooBig.
func (c *Conn) SetReadLimit(n int64) {
	c.msgReader.limitReader.limit.Store(n)
}

func newMsgReader(c *Conn) *msgReader {
	mr := &msgReader{
		c:   c,
		fin: true,
	}

	mr.limitReader = newLimitReader(c, readerFunc(mr.read), 32768)
	return mr
}

func (mr *msgReader) initFlateReader() {
	mr.flateReader = getFlateReader(readerFunc(mr.read))
	mr.limitReader.r = mr.flateReader
}

func (mr *msgReader) close() {
	mr.c.readMu.Lock(context.Background())
	defer mr.c.readMu.Unlock()

	mr.returnFlateReader()
}

func (mr *msgReader) flateContextTakeover() bool {
	if mr.c.client {
		return !mr.c.copts.serverNoContextTakeover
	}
	return !mr.c.copts.clientNoContextTakeover
}

func (c *Conn) readRSV1Illegal(h header) bool {
	// If compression is enabled, rsv1 is always illegal.
	if !c.flate() {
		return true
	}
	// rsv1 is only allowed on data frames beginning messages.
	if h.opcode != opText && h.opcode != opBinary {
		return true
	}
	return false
}

func (c *Conn) readLoop(ctx context.Context) (header, error) {
	for {
		h, err := c.readFrameHeader(ctx)
		if err != nil {
			return header{}, err
		}

		if h.rsv1 && c.readRSV1Illegal(h) || h.rsv2 || h.rsv3 {
			err := xerrors.Errorf("received header with unexpected rsv bits set: %v:%v:%v", h.rsv1, h.rsv2, h.rsv3)
			c.writeError(StatusProtocolError, err)
			return header{}, err
		}

		if !c.client && !h.masked {
			return header{}, xerrors.New("received unmasked frame from client")
		}

		switch h.opcode {
		case opClose, opPing, opPong:
			err = c.handleControl(ctx, h)
			if err != nil {
				// Pass through CloseErrors when receiving a close frame.
				if h.opcode == opClose && CloseStatus(err) != -1 {
					return header{}, err
				}
				return header{}, xerrors.Errorf("failed to handle control frame %v: %w", h.opcode, err)
			}
		case opContinuation, opText, opBinary:
			return h, nil
		default:
			err := xerrors.Errorf("received unknown opcode %v", h.opcode)
			c.writeError(StatusProtocolError, err)
			return header{}, err
		}
	}
}

func (c *Conn) readFrameHeader(ctx context.Context) (header, error) {
	select {
	case <-c.closed:
		return header{}, c.closeErr
	case c.readTimeout <- ctx:
	}

	h, err := readFrameHeader(c.br)
	if err != nil {
		select {
		case <-c.closed:
			return header{}, c.closeErr
		case <-ctx.Done():
			return header{}, ctx.Err()
		default:
			c.close(err)
			return header{}, err
		}
	}

	select {
	case <-c.closed:
		return header{}, c.closeErr
	case c.readTimeout <- context.Background():
	}

	return h, nil
}

func (c *Conn) readFramePayload(ctx context.Context, p []byte) (int, error) {
	select {
	case <-c.closed:
		return 0, c.closeErr
	case c.readTimeout <- ctx:
	}

	n, err := io.ReadFull(c.br, p)
	if err != nil {
		select {
		case <-c.closed:
			return n, c.closeErr
		case <-ctx.Done():
			return n, ctx.Err()
		default:
			err = xerrors.Errorf("failed to read frame payload: %w", err)
			c.close(err)
			return n, err
		}
	}

	select {
	case <-c.closed:
		return n, c.closeErr
	case c.readTimeout <- context.Background():
	}

	return n, err
}

func (c *Conn) handleControl(ctx context.Context, h header) (err error) {
	if h.payloadLength < 0 || h.payloadLength > maxControlPayload {
		err := xerrors.Errorf("received control frame payload with invalid length: %d", h.payloadLength)
		c.writeError(StatusProtocolError, err)
		return err
	}

	if !h.fin {
		err := xerrors.New("received fragmented control frame")
		c.writeError(StatusProtocolError, err)
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	b := c.readControlBuf[:h.payloadLength]
	_, err = c.readFramePayload(ctx, b)
	if err != nil {
		return err
	}

	if h.masked {
		mask(h.maskKey, b)
	}

	switch h.opcode {
	case opPing:
		return c.writeControl(ctx, opPong, b)
	case opPong:
		c.activePingsMu.Lock()
		pong, ok := c.activePings[string(b)]
		c.activePingsMu.Unlock()
		if ok {
			close(pong)
		}
		return nil
	}

	defer func() {
		c.readCloseFrameErr = err
	}()

	ce, err := parseClosePayload(b)
	if err != nil {
		err = xerrors.Errorf("received invalid close payload: %w", err)
		c.writeError(StatusProtocolError, err)
		return err
	}

	err = xerrors.Errorf("received close frame: %w", ce)
	c.setCloseErr(err)
	c.writeClose(ce.Code, ce.Reason)
	c.close(err)
	return err
}

func (c *Conn) reader(ctx context.Context) (_ MessageType, _ io.Reader, err error) {
	defer errd.Wrap(&err, "failed to get reader")

	err = c.readMu.Lock(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer c.readMu.Unlock()

	if !c.msgReader.fin {
		return 0, nil, xerrors.New("previous message not read to completion")
	}

	h, err := c.readLoop(ctx)
	if err != nil {
		return 0, nil, err
	}

	if h.opcode == opContinuation {
		err := xerrors.New("received continuation frame without text or binary frame")
		c.writeError(StatusProtocolError, err)
		return 0, nil, err
	}

	c.msgReader.reset(ctx, h)

	return MessageType(h.opcode), c.msgReader, nil
}

type msgReader struct {
	c *Conn

	ctx         context.Context
	deflate     bool
	flateReader io.Reader
	deflateTail strings.Reader
	limitReader *limitReader

	fin           bool
	payloadLength int64
	maskKey       uint32
}

func (mr *msgReader) reset(ctx context.Context, h header) {
	mr.ctx = ctx
	mr.deflate = h.rsv1
	if mr.deflate {
		if !mr.flateContextTakeover() {
			mr.initFlateReader()
		}
		mr.deflateTail.Reset(deflateMessageTail)
	}

	mr.limitReader.reset()
	mr.setFrame(h)
}

func (mr *msgReader) setFrame(h header) {
	mr.fin = h.fin
	mr.payloadLength = h.payloadLength
	mr.maskKey = h.maskKey
}

func (mr *msgReader) Read(p []byte) (n int, err error) {
	defer func() {
		r := recover()
		if r != nil {
			if r != "ANMOL" {
				panic(r)
			}
			err = io.EOF
			if !mr.flateContextTakeover() {
				mr.returnFlateReader()
			}
		}

		errd.Wrap(&err, "failed to read")
		if xerrors.Is(err, io.EOF) {
			err = io.EOF
		}
	}()

	err = mr.c.readMu.Lock(mr.ctx)
	if err != nil {
		return 0, err
	}
	defer mr.c.readMu.Unlock()

	return mr.limitReader.Read(p)
}

func (mr *msgReader) returnFlateReader() {
	if mr.flateReader != nil {
		putFlateReader(mr.flateReader)
		mr.flateReader = nil
	}
}

func (mr *msgReader) read(p []byte) (int, error) {
	if mr.payloadLength == 0 {
		if mr.fin {
			if mr.deflate {
				if mr.deflateTail.Len() == 0 {
					panic("ANMOL")
				}
				n, _ := mr.deflateTail.Read(p)
				return n, nil
			}
			return 0, io.EOF
		}

		h, err := mr.c.readLoop(mr.ctx)
		if err != nil {
			return 0, err
		}
		if h.opcode != opContinuation {
			err := xerrors.New("received new data message without finishing the previous message")
			mr.c.writeError(StatusProtocolError, err)
			return 0, err
		}
		mr.setFrame(h)
	}

	if int64(len(p)) > mr.payloadLength {
		p = p[:mr.payloadLength]
	}

	n, err := mr.c.readFramePayload(mr.ctx, p)
	if err != nil {
		return n, err
	}

	mr.payloadLength -= int64(n)

	if !mr.c.client {
		mr.maskKey = mask(mr.maskKey, p)
	}

	return n, nil
}

type limitReader struct {
	c     *Conn
	r     io.Reader
	limit atomicInt64
	n     int64
}

func newLimitReader(c *Conn, r io.Reader, limit int64) *limitReader {
	lr := &limitReader{
		c: c,
	}
	lr.limit.Store(limit)
	lr.r = r
	lr.reset()
	return lr
}

func (lr *limitReader) reset() {
	lr.n = lr.limit.Load()
}

func (lr *limitReader) Read(p []byte) (int, error) {
	if lr.n <= 0 {
		err := xerrors.Errorf("read limited at %v bytes", lr.limit.Load())
		lr.c.writeError(StatusMessageTooBig, err)
		return 0, err
	}

	if int64(len(p)) > lr.n {
		p = p[:lr.n]
	}
	n, err := lr.r.Read(p)
	lr.n -= int64(n)
	return n, err
}

type atomicInt64 struct {
	// We do not use atomic.Load/StoreInt64 since it does not
	// work on 32 bit computers but we need 64 bit integers.
	i atomic.Value
}

func (v *atomicInt64) Load() int64 {
	i, _ := v.i.Load().(int64)
	return i
}

func (v *atomicInt64) Store(i int64) {
	v.i.Store(i)
}

type readerFunc func(p []byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) {
	return f(p)
}
