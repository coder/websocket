// +build !js

package websocket

import (
	"bufio"
	"compress/flate"
	"context"
	"crypto/rand"
	"encoding/binary"
	"io"
	"time"

	"golang.org/x/xerrors"

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
	w, err := c.writer(ctx, typ)
	if err != nil {
		return nil, xerrors.Errorf("failed to get writer: %w", err)
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
	_, err := c.write(ctx, typ, p)
	if err != nil {
		return xerrors.Errorf("failed to write msg: %w", err)
	}
	return nil
}

func newMsgWriter(c *Conn) *msgWriter {
	mw := &msgWriter{
		c:  c,
		mu: newMu(c),
	}
	mw.trimWriter = &trimLastFourBytesWriter{
		w: writerFunc(mw.write),
	}
	return mw
}

func (mw *msgWriter) ensureFlateWriter() {
	if mw.flateWriter == nil {
		mw.flateWriter = getFlateWriter(mw.trimWriter, nil)
	}
}

func (mw *msgWriter) flateContextTakeover() bool {
	if mw.c.client {
		return !mw.c.copts.clientNoContextTakeover
	}
	return !mw.c.copts.serverNoContextTakeover
}

func (c *Conn) writer(ctx context.Context, typ MessageType) (io.WriteCloser, error) {
	err := c.msgWriter.reset(ctx, typ)
	if err != nil {
		return nil, err
	}
	return c.msgWriter, nil
}

func (c *Conn) write(ctx context.Context, typ MessageType, p []byte) (int, error) {
	mw, err := c.writer(ctx, typ)
	if err != nil {
		return 0, err
	}

	if !c.flate() {
		// Fast single frame path.
		defer c.msgWriter.mu.Unlock()
		return c.writeFrame(ctx, true, false, c.msgWriter.opcode, p)
	}

	n, err := mw.Write(p)
	if err != nil {
		return n, err
	}

	err = mw.Close()
	return n, err
}

type msgWriter struct {
	c *Conn

	mu *mu

	ctx    context.Context
	opcode opcode
	closed bool

	flate       bool
	trimWriter  *trimLastFourBytesWriter
	flateWriter *flate.Writer
}

func (mw *msgWriter) reset(ctx context.Context, typ MessageType) error {
	err := mw.mu.Lock(ctx)
	if err != nil {
		return err
	}

	mw.closed = false
	mw.ctx = ctx
	mw.opcode = opcode(typ)
	mw.flate = false
	return nil
}

// Write writes the given bytes to the WebSocket connection.
func (mw *msgWriter) Write(p []byte) (_ int, err error) {
	defer errd.Wrap(&err, "failed to write")

	if mw.closed {
		return 0, xerrors.New("cannot use closed writer")
	}

	if mw.c.flate() {
		if !mw.flate {
			mw.flate = true

			if !mw.flateContextTakeover() {
				mw.ensureFlateWriter()
			}
			mw.trimWriter.reset()
		}

		return mw.flateWriter.Write(p)
	}

	return mw.write(p)
}

func (mw *msgWriter) write(p []byte) (int, error) {
	n, err := mw.c.writeFrame(mw.ctx, false, mw.flate, mw.opcode, p)
	if err != nil {
		return n, xerrors.Errorf("failed to write data frame: %w", err)
	}
	mw.opcode = opContinuation
	return n, nil
}

// Close flushes the frame to the connection.
func (mw *msgWriter) Close() (err error) {
	defer errd.Wrap(&err, "failed to close writer")

	if mw.closed {
		return xerrors.New("cannot use closed writer")
	}
	mw.closed = true

	if mw.flate {
		err = mw.flateWriter.Flush()
		if err != nil {
			return xerrors.Errorf("failed to flush flate writer: %w", err)
		}
	}

	_, err = mw.c.writeFrame(mw.ctx, true, mw.flate, mw.opcode, nil)
	if err != nil {
		return xerrors.Errorf("failed to write fin frame: %w", err)
	}

	if mw.c.flate() && !mw.flateContextTakeover() && mw.flateWriter != nil {
		putFlateWriter(mw.flateWriter)
		mw.flateWriter = nil
	}

	mw.mu.Unlock()
	return nil
}

func (mw *msgWriter) close() {
	if mw.flateWriter != nil && mw.flateContextTakeover() {
		mw.mu.Lock(context.Background())
		putFlateWriter(mw.flateWriter)
		mw.flateWriter = nil
	}
}

func (c *Conn) writeControl(ctx context.Context, opcode opcode, p []byte) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	_, err := c.writeFrame(ctx, true, false, opcode, p)
	if err != nil {
		return xerrors.Errorf("failed to write control frame %v: %w", opcode, err)
	}
	return nil
}

// frame handles all writes to the connection.
func (c *Conn) writeFrame(ctx context.Context, fin bool, flate bool, opcode opcode, p []byte) (int, error) {
	err := c.writeFrameMu.Lock(ctx)
	if err != nil {
		return 0, err
	}
	defer c.writeFrameMu.Unlock()

	select {
	case <-c.closed:
		return 0, c.closeErr
	case c.writeTimeout <- ctx:
	}

	c.writeHeader.fin = fin
	c.writeHeader.opcode = opcode
	c.writeHeader.payloadLength = int64(len(p))

	if c.client {
		c.writeHeader.masked = true
		err = binary.Read(rand.Reader, binary.LittleEndian, &c.writeHeader.maskKey)
		if err != nil {
			return 0, xerrors.Errorf("failed to generate masking key: %w", err)
		}
	}

	c.writeHeader.rsv1 = false
	if flate && (opcode == opText || opcode == opBinary) {
		c.writeHeader.rsv1 = true
	}

	err = writeFrameHeader(c.writeHeader, c.bw)
	if err != nil {
		return 0, err
	}

	n, err := c.writeFramePayload(p)
	if err != nil {
		return n, err
	}

	if c.writeHeader.fin {
		err = c.bw.Flush()
		if err != nil {
			return n, xerrors.Errorf("failed to flush: %w", err)
		}
	}

	select {
	case <-c.closed:
		return n, c.closeErr
	case c.writeTimeout <- context.Background():
	}

	return n, nil
}

func (c *Conn) writeFramePayload(p []byte) (_ int, err error) {
	defer errd.Wrap(&err, "failed to write frame payload")

	if !c.writeHeader.masked {
		return c.bw.Write(p)
	}

	var n int
	maskKey := c.writeHeader.maskKey
	for len(p) > 0 {
		// If the buffer is full, we need to flush.
		if c.bw.Available() == 0 {
			err = c.bw.Flush()
			if err != nil {
				return n, err
			}
		}

		// Start of next write in the buffer.
		i := c.bw.Buffered()

		j := len(p)
		if j > c.bw.Available() {
			j = c.bw.Available()
		}

		_, err := c.bw.Write(p[:j])
		if err != nil {
			return n, err
		}

		maskKey = mask(maskKey, c.writeBuf[i:c.bw.Buffered()])

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
