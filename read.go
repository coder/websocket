package websocket

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"nhooyr.io/websocket/internal/errd"
	"strings"
	"sync/atomic"
	"time"
)

// Reader waits until there is a WebSocket data message to read
// from the connection.
// It returns the type of the message and a reader to read it.
// The passed context will also bound the reader.
// Ensure you read to EOF otherwise the connection will hang.
//
// All returned errors will cause the connection
// to be closed so you do not need to write your own error message.
// This applies to the Read methods in the wsjson/wspb subpackages as well.
//
// You must read from the connection for control frames to be handled.
// Thus if you expect messages to take a long time to be responded to,
// you should handle such messages async to reading from the connection
// to ensure control frames are promptly handled.
//
// If you do not expect any data messages from the peer, call CloseRead.
//
// Only one Reader may be open at a time.
//
// If you need a separate timeout on the Reader call and then the message
// Read, use time.AfterFunc to cancel the context passed in early.
// See https://github.com/nhooyr/websocket/issues/87#issue-451703332
// Most users should not need this.
func (c *Conn) Reader(ctx context.Context) (MessageType, io.Reader, error) {
	typ, r, err := c.cr.reader(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to get reader: %w", err)
	}
	return typ, r, nil
}

// Read is a convenience method to read a single message from the connection.
//
// See the Reader method to reuse buffers or for streaming.
// The docs on Reader apply to this method as well.
func (c *Conn) Read(ctx context.Context) (MessageType, []byte, error) {
	typ, r, err := c.Reader(ctx)
	if err != nil {
		return 0, nil, err
	}

	b, err := ioutil.ReadAll(r)
	return typ, b, err
}

// CloseRead will start a goroutine to read from the connection until it is closed or a data message
// is received. If a data message is received, the connection will be closed with StatusPolicyViolation.
// Since CloseRead reads from the connection, it will respond to ping, pong and close frames.
// After calling this method, you cannot read any data messages from the connection.
// The returned context will be cancelled when the connection is closed.
//
// Use this when you do not want to read data messages from the connection anymore but will
// want to write messages to it.
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
	c.cr.mr.lr.limit.Store(n)
}

type connReader struct {
	c       *Conn
	br      *bufio.Reader
	timeout chan context.Context

	mu                mu
	controlPayloadBuf [maxControlPayload]byte
	mr                *msgReader
}

func (cr *connReader) init(c *Conn, br *bufio.Reader) {
	cr.c = c
	cr.br = br
	cr.timeout = make(chan context.Context)

	cr.mr = &msgReader{
		cr:  cr,
		fin: true,
	}

	cr.mr.lr = newLimitReader(c, readerFunc(cr.mr.read), 32768)
	if c.deflateNegotiated() && cr.contextTakeover() {
		cr.ensureFlateReader()
	}
}

func (cr *connReader) ensureFlateReader() {
	cr.mr.fr = getFlateReader(readerFunc(cr.mr.read))
	cr.mr.lr.reset(cr.mr.fr)
}

func (cr *connReader) close() {
	cr.mu.Lock(context.Background())
	if cr.c.client {
		putBufioReader(cr.br)
	}
	if cr.c.deflateNegotiated() && cr.contextTakeover() {
		putFlateReader(cr.mr.fr)
	}
}

func (cr *connReader) contextTakeover() bool {
	if cr.c.client {
		return cr.c.copts.serverNoContextTakeover
	}
	return cr.c.copts.clientNoContextTakeover
}

func (cr *connReader) rsv1Illegal(h header) bool {
	// If compression is enabled, rsv1 is always illegal.
	if !cr.c.deflateNegotiated() {
		return true
	}
	// rsv1 is only allowed on data frames beginning messages.
	if h.opcode != opText && h.opcode != opBinary {
		return true
	}
	return false
}

func (cr *connReader) loop(ctx context.Context) (header, error) {
	for {
		h, err := cr.frameHeader(ctx)
		if err != nil {
			return header{}, err
		}

		if h.rsv1 && cr.rsv1Illegal(h) || h.rsv2 || h.rsv3 {
			err := fmt.Errorf("received header with unexpected rsv bits set: %v:%v:%v", h.rsv1, h.rsv2, h.rsv3)
			cr.c.cw.error(StatusProtocolError, err)
			return header{}, err
		}

		if !cr.c.client && !h.masked {
			return header{}, errors.New("received unmasked frame from client")
		}

		switch h.opcode {
		case opClose, opPing, opPong:
			err = cr.control(ctx, h)
			if err != nil {
				// Pass through CloseErrors when receiving a close frame.
				if h.opcode == opClose && CloseStatus(err) != -1 {
					return header{}, err
				}
				return header{}, fmt.Errorf("failed to handle control frame %v: %w", h.opcode, err)
			}
		case opContinuation, opText, opBinary:
			return h, nil
		default:
			err := fmt.Errorf("received unknown opcode %v", h.opcode)
			cr.c.cw.error(StatusProtocolError, err)
			return header{}, err
		}
	}
}

func (cr *connReader) frameHeader(ctx context.Context) (header, error) {
	select {
	case <-cr.c.closed:
		return header{}, cr.c.closeErr
	case cr.timeout <- ctx:
	}

	h, err := readFrameHeader(cr.br)
	if err != nil {
		select {
		case <-cr.c.closed:
			return header{}, cr.c.closeErr
		case <-ctx.Done():
			return header{}, ctx.Err()
		default:
			cr.c.close(err)
			return header{}, err
		}
	}

	select {
	case <-cr.c.closed:
		return header{}, cr.c.closeErr
	case cr.timeout <- context.Background():
	}

	return h, nil
}

func (cr *connReader) framePayload(ctx context.Context, p []byte) (int, error) {
	select {
	case <-cr.c.closed:
		return 0, cr.c.closeErr
	case cr.timeout <- ctx:
	}

	n, err := io.ReadFull(cr.br, p)
	if err != nil {
		select {
		case <-cr.c.closed:
			return n, cr.c.closeErr
		case <-ctx.Done():
			return n, ctx.Err()
		default:
			err = fmt.Errorf("failed to read frame payload: %w", err)
			cr.c.close(err)
			return n, err
		}
	}

	select {
	case <-cr.c.closed:
		return n, cr.c.closeErr
	case cr.timeout <- context.Background():
	}

	return n, err
}

func (cr *connReader) control(ctx context.Context, h header) error {
	if h.payloadLength < 0 {
		err := fmt.Errorf("received header with negative payload length: %v", h.payloadLength)
		cr.c.cw.error(StatusProtocolError, err)
		return err
	}

	if h.payloadLength > maxControlPayload {
		err := fmt.Errorf("received too big control frame at %v bytes", h.payloadLength)
		cr.c.cw.error(StatusProtocolError, err)
		return err
	}

	if !h.fin {
		err := errors.New("received fragmented control frame")
		cr.c.cw.error(StatusProtocolError, err)
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	b := cr.controlPayloadBuf[:h.payloadLength]
	_, err := cr.framePayload(ctx, b)
	if err != nil {
		return err
	}

	if h.masked {
		mask(h.maskKey, b)
	}

	switch h.opcode {
	case opPing:
		return cr.c.cw.control(ctx, opPong, b)
	case opPong:
		cr.c.activePingsMu.Lock()
		pong, ok := cr.c.activePings[string(b)]
		cr.c.activePingsMu.Unlock()
		if ok {
			close(pong)
		}
		return nil
	}

	ce, err := parseClosePayload(b)
	if err != nil {
		err = fmt.Errorf("received invalid close payload: %w", err)
		cr.c.cw.error(StatusProtocolError, err)
		return err
	}

	err = fmt.Errorf("received close frame: %w", ce)
	cr.c.setCloseErr(err)
	cr.c.cw.control(context.Background(), opClose, ce.bytes())
	return err
}

func (cr *connReader) reader(ctx context.Context) (MessageType, io.Reader, error) {
	err := cr.mu.Lock(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer cr.mu.Unlock()

	if !cr.mr.fin {
		return 0, nil, errors.New("previous message not read to completion")
	}

	h, err := cr.loop(ctx)
	if err != nil {
		return 0, nil, err
	}

	if h.opcode == opContinuation {
		err := errors.New("received continuation frame without text or binary frame")
		cr.c.cw.error(StatusProtocolError, err)
		return 0, nil, err
	}

	cr.mr.reset(ctx, h)

	return MessageType(h.opcode), cr.mr, nil
}

type msgReader struct {
	cr *connReader
	fr io.Reader
	lr *limitReader

	ctx context.Context

	deflate     bool
	deflateTail strings.Reader

	payloadLength int64
	maskKey       uint32
	fin           bool
}

func (mr *msgReader) reset(ctx context.Context, h header) {
	mr.ctx = ctx
	mr.deflate = h.rsv1
	if mr.deflate {
		mr.deflateTail.Reset(deflateMessageTail)
		if !mr.cr.contextTakeover() {
			mr.cr.ensureFlateReader()
		}
	}
	mr.setFrame(h)
	mr.fin = false
}

func (mr *msgReader) setFrame(h header) {
	mr.payloadLength = h.payloadLength
	mr.maskKey = h.maskKey
	mr.fin = h.fin
}

func (mr *msgReader) Read(p []byte) (_ int, err error) {
	defer func() {
		errd.Wrap(&err, "failed to read")
		if errors.Is(err, io.EOF) {
			err = io.EOF
		}
	}()

	err = mr.cr.mu.Lock(mr.ctx)
	if err != nil {
		return 0, err
	}
	defer mr.cr.mu.Unlock()

	if mr.payloadLength == 0 && mr.fin {
		if mr.cr.c.deflateNegotiated() && !mr.cr.contextTakeover() {
			if mr.fr != nil {
				putFlateReader(mr.fr)
				mr.fr = nil
			}
		}
		return 0, io.EOF
	}

	return mr.lr.Read(p)
}

func (mr *msgReader) read(p []byte) (int, error) {
	log.Println("compress", mr.deflate)

	if mr.payloadLength == 0 {
		h, err := mr.cr.loop(mr.ctx)
		if err != nil {
			return 0, err
		}
		if h.opcode != opContinuation {
			err := errors.New("received new data message without finishing the previous message")
			mr.cr.c.cw.error(StatusProtocolError, err)
			return 0, err
		}
		mr.setFrame(h)
	}

	if int64(len(p)) > mr.payloadLength {
		p = p[:mr.payloadLength]
	}

	n, err := mr.cr.framePayload(mr.ctx, p)
	if err != nil {
		return n, err
	}

	mr.payloadLength -= int64(n)

	if !mr.cr.c.client {
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
	lr.reset(r)
	return lr
}

func (lr *limitReader) reset(r io.Reader) {
	lr.n = lr.limit.Load()
	lr.r = r
}

func (lr *limitReader) Read(p []byte) (int, error) {
	if lr.n <= 0 {
		err := fmt.Errorf("read limited at %v bytes", lr.limit.Load())
		lr.c.cw.error(StatusMessageTooBig, err)
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
