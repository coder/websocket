package websocket

import (
	"bufio"
	"compress/flate"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"nhooyr.io/websocket/internal/errd"
)

// Writer returns a writer bounded by the context that will write
// a WebSocket message of type dataType to the connection.
//
// You must close the writer once you have written the entire message.
//
// Only one writer can be open at a time, multiple calls will block until the previous writer
// is closed.
//
// Never close the returned writer twice.
func (c *Conn) Writer(ctx context.Context, typ MessageType) (io.WriteCloser, error) {
	w, err := c.cw.writer(ctx, typ)
	if err != nil {
		return nil, fmt.Errorf("failed to get writer: %w", err)
	}
	return w, nil
}

// Write writes a message to the connection.
//
// See the Writer method if you want to stream a message.
//
// If compression is disabled, then it is guaranteed to write the message
// in a single frame.
func (c *Conn) Write(ctx context.Context, typ MessageType, p []byte) error {
	_, err := c.cw.write(ctx, typ, p)
	if err != nil {
		return fmt.Errorf("failed to write msg: %w", err)
	}
	return nil
}

type connWriter struct {
	c  *Conn
	bw *bufio.Writer

	writeBuf []byte

	mw      *messageWriter
	frameMu mu
	h       header

	timeout chan context.Context
}

func (cw *connWriter) init(c *Conn, bw *bufio.Writer) {
	cw.c = c
	cw.bw = bw

	if cw.c.client {
		cw.writeBuf = extractBufioWriterBuf(cw.bw, c.rwc)
	}

	cw.timeout = make(chan context.Context)

	cw.mw = &messageWriter{
		cw: cw,
	}
	cw.mw.tw = &trimLastFourBytesWriter{
		w: writerFunc(cw.mw.write),
	}
	if cw.c.deflateNegotiated() && cw.mw.contextTakeover() {
		cw.mw.ensureFlateWriter()
	}
}

func (mw *messageWriter) ensureFlateWriter() {
	mw.fw = getFlateWriter(mw.tw)
}

func (cw *connWriter) close() {
	if cw.c.client {
		cw.frameMu.Lock(context.Background())
		putBufioWriter(cw.bw)
	}
	if cw.c.deflateNegotiated() && cw.mw.contextTakeover() {
		cw.mw.mu.Lock(context.Background())
		putFlateWriter(cw.mw.fw)
	}
}

func (mw *messageWriter) contextTakeover() bool {
	if mw.cw.c.client {
		return mw.cw.c.copts.clientNoContextTakeover
	}
	return mw.cw.c.copts.serverNoContextTakeover
}

func (cw *connWriter) writer(ctx context.Context, typ MessageType) (io.WriteCloser, error) {
	err := cw.mw.reset(ctx, typ)
	if err != nil {
		return nil, err
	}
	return cw.mw, nil
}

func (cw *connWriter) write(ctx context.Context, typ MessageType, p []byte) (int, error) {
	ww, err := cw.writer(ctx, typ)
	if err != nil {
		return 0, err
	}

	if !cw.c.deflateNegotiated() {
		// Fast single frame path.
		defer cw.mw.mu.Unlock()
		return cw.frame(ctx, true, cw.mw.opcode, p)
	}

	n, err := ww.Write(p)
	if err != nil {
		return n, err
	}

	err = ww.Close()
	return n, err
}

type messageWriter struct {
	cw *connWriter

	mu       mu
	compress bool
	tw       *trimLastFourBytesWriter
	fw       *flate.Writer
	ctx      context.Context
	opcode   opcode
	closed   bool
}

func (mw *messageWriter) reset(ctx context.Context, typ MessageType) error {
	err := mw.mu.Lock(ctx)
	if err != nil {
		return err
	}

	mw.closed = false
	mw.ctx = ctx
	mw.opcode = opcode(typ)
	return nil
}

// Write writes the given bytes to the WebSocket connection.
func (mw *messageWriter) Write(p []byte) (_ int, err error) {
	defer errd.Wrap(&err, "failed to write")

	if mw.closed {
		return 0, errors.New("cannot use closed writer")
	}

	if mw.cw.c.deflateNegotiated() {
		if !mw.compress {
			if !mw.contextTakeover() {
				mw.ensureFlateWriter()
			}
			mw.tw.reset()
			mw.compress = true
		}

		return mw.fw.Write(p)
	}

	return mw.write(p)
}

func (mw *messageWriter) write(p []byte) (int, error) {
	n, err := mw.cw.frame(mw.ctx, false, mw.opcode, p)
	if err != nil {
		return n, fmt.Errorf("failed to write data frame: %w", err)
	}
	mw.opcode = opContinuation
	return n, nil
}

// Close flushes the frame to the connection.
// This must be called for every messageWriter.
func (mw *messageWriter) Close() (err error) {
	defer errd.Wrap(&err, "failed to close writer")

	if mw.closed {
		return errors.New("cannot use closed writer")
	}
	mw.closed = true

	if mw.cw.c.deflateNegotiated() {
		err = mw.fw.Flush()
		if err != nil {
			return fmt.Errorf("failed to flush flate writer: %w", err)
		}
	}

	_, err = mw.cw.frame(mw.ctx, true, mw.opcode, nil)
	if err != nil {
		return fmt.Errorf("failed to write fin frame: %w", err)
	}

	if mw.compress && !mw.contextTakeover() {
		putFlateWriter(mw.fw)
		mw.compress = false
	}

	mw.mu.Unlock()
	return nil
}

func (cw *connWriter) control(ctx context.Context, opcode opcode, p []byte) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	_, err := cw.frame(ctx, true, opcode, p)
	if err != nil {
		return fmt.Errorf("failed to write control frame %v: %w", opcode, err)
	}
	return nil
}

// frame handles all writes to the connection.
func (cw *connWriter) frame(ctx context.Context, fin bool, opcode opcode, p []byte) (int, error) {
	err := cw.frameMu.Lock(ctx)
	if err != nil {
		return 0, err
	}
	defer cw.frameMu.Unlock()

	select {
	case <-cw.c.closed:
		return 0, cw.c.closeErr
	case cw.timeout <- ctx:
	}

	cw.h.fin = fin
	cw.h.opcode = opcode
	cw.h.masked = cw.c.client
	cw.h.payloadLength = int64(len(p))

	cw.h.rsv1 = false
	if cw.mw.compress && (opcode == opText || opcode == opBinary) {
		cw.h.rsv1 = true
	}

	if cw.h.masked {
		err = binary.Read(rand.Reader, binary.LittleEndian, &cw.h.maskKey)
		if err != nil {
			return 0, fmt.Errorf("failed to generate masking key: %w", err)
		}
	}

	err = writeFrameHeader(cw.h, cw.bw)
	if err != nil {
		return 0, err
	}

	n, err := cw.framePayload(p)
	if err != nil {
		return n, err
	}

	if cw.h.fin {
		err = cw.bw.Flush()
		if err != nil {
			return n, fmt.Errorf("failed to flush: %w", err)
		}
	}

	select {
	case <-cw.c.closed:
		return n, cw.c.closeErr
	case cw.timeout <- context.Background():
	}

	return n, nil
}

func (cw *connWriter) framePayload(p []byte) (_ int, err error) {
	defer errd.Wrap(&err, "failed to write frame payload")

	if !cw.h.masked {
		return cw.bw.Write(p)
	}

	var n int
	maskKey := cw.h.maskKey
	for len(p) > 0 {
		// If the buffer is full, we need to flush.
		if cw.bw.Available() == 0 {
			err = cw.bw.Flush()
			if err != nil {
				return n, err
			}
		}

		// Start of next write in the buffer.
		i := cw.bw.Buffered()

		j := len(p)
		if j > cw.bw.Available() {
			j = cw.bw.Available()
		}

		_, err := cw.bw.Write(p[:j])
		if err != nil {
			return n, err
		}

		maskKey = mask(maskKey, cw.writeBuf[i:cw.bw.Buffered()])

		p = p[j:]
		n += j
	}

	return n, nil
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}

// extractBufioWriterBuf grabs the []byte backing a *bufio.Writer
// and returns it.
func extractBufioWriterBuf(bw *bufio.Writer, w io.Writer) []byte {
	var writeBuf []byte
	bw.Reset(writerFunc(func(p2 []byte) (int, error) {
		writeBuf = p2[:cap(p2)]
		return len(p2), nil
	}))

	bw.WriteByte(0)
	bw.Flush()

	bw.Reset(w)

	return writeBuf
}
