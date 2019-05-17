package websocket

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/xerrors"
)

type frame struct {
	opcode  opcode
	payload []byte
}

// Conn represents a WebSocket connection.
// All methods except Reader can be used concurrently.
// Please be sure to call Close on the connection when you
// are finished with it to release resources.
type Conn struct {
	subprotocol string
	br          *bufio.Reader
	bw          *bufio.Writer
	closer      io.Closer
	client      bool

	closeOnce sync.Once
	closeErr  error
	closed    chan struct{}

	// Writers should send on write to begin sending
	// a message and then follow that up with some data
	// on writeBytes.
	// Send on control to write a control message.
	// writeDone will be sent back when the message is written
	// Send on writeFlush to flush the message and wait for a
	// ping on writeDone.
	// writeDone will be closed if the data message write errors.
	write      chan MessageType
	control    chan frame
	fastWrite  chan frame
	writeBytes chan []byte
	writeDone  chan struct{}
	writeFlush chan struct{}

	// Readers should receive on read to begin reading a message.
	// Then send a byte slice to readBytes to read into it.
	// The n of bytes read will be sent on readDone once the read into a slice is complete.
	// readDone will be closed if the read fails.
	// activeReader will be set to 0 on io.EOF.
	activeReader int64
	inMsg        bool
	read         chan opcode
	readBytes    chan []byte
	readDone     chan int
}

func (c *Conn) close(err error) {
	c.closeOnce.Do(func() {
		runtime.SetFinalizer(c, nil)

		cerr := c.closer.Close()
		if err != nil {
			cerr = err
		}

		c.closeErr = xerrors.Errorf("websocket closed: %w", cerr)

		close(c.closed)
	})
}

// Subprotocol returns the negotiated subprotocol.
// An empty string means the default protocol.
func (c *Conn) Subprotocol() string {
	return c.subprotocol
}

func (c *Conn) init() {
	c.closed = make(chan struct{})

	c.write = make(chan MessageType)
	c.control = make(chan frame)
	c.fastWrite = make(chan frame)
	c.writeBytes = make(chan []byte)
	c.writeDone = make(chan struct{})
	c.writeFlush = make(chan struct{})

	c.read = make(chan opcode)
	c.readBytes = make(chan []byte)
	c.readDone = make(chan int)

	runtime.SetFinalizer(c, func(c *Conn) {
		c.close(xerrors.New("connection garbage collected"))
	})

	go c.writeLoop()
	go c.readLoop()
}

// We never mask inside here because our mask key is always 0,0,0,0.
// See comment on secWebSocketKey.
func (c *Conn) writeFrame(h header, p []byte) {
	b2 := marshalHeader(h)
	_, err := c.bw.Write(b2)
	if err != nil {
		c.close(xerrors.Errorf("failed to write to connection: %w", err))
		return
	}

	_, err = c.bw.Write(p)
	if err != nil {
		c.close(xerrors.Errorf("failed to write to connection: %w", err))
		return
	}

	if h.fin {
		err := c.bw.Flush()
		if err != nil {
			c.close(xerrors.Errorf("failed to write to connection: %w", err))
			return
		}
	}
}

func (c *Conn) writeLoopFastWrite(frame frame) {
	h := header{
		fin:           true,
		opcode:        frame.opcode,
		payloadLength: int64(len(frame.payload)),
		masked:        c.client,
	}
	c.writeFrame(h, frame.payload)
	select {
	case <-c.closed:
	case c.writeDone <- struct{}{}:
	}
}

func (c *Conn) writeLoop() {
	defer close(c.writeDone)

messageLoop:
	for {
		var dataType MessageType
		select {
		case <-c.closed:
			return
		case control := <-c.control:
			c.writeLoopFastWrite(control)
			continue
		case frame := <-c.fastWrite:
			c.writeLoopFastWrite(frame)
			continue
		case dataType = <-c.write:
		}

		var firstSent bool
		for {
			select {
			case <-c.closed:
				return
			case control := <-c.control:
				c.writeLoopFastWrite(control)
			case b := <-c.writeBytes:
				h := header{
					fin:           false,
					opcode:        opcode(dataType),
					payloadLength: int64(len(b)),
					masked:        c.client,
				}

				if firstSent {
					h.opcode = opContinuation
				}
				firstSent = true

				c.writeFrame(h, b)

				select {
				case <-c.closed:
					return
				case c.writeDone <- struct{}{}:
				}
			case <-c.writeFlush:
				h := header{
					fin:           true,
					opcode:        opcode(dataType),
					payloadLength: 0,
					masked:        c.client,
				}

				if firstSent {
					h.opcode = opContinuation
				}

				c.writeFrame(h, nil)

				continue messageLoop
			}
		}
	}
}

func (c *Conn) handleControl(h header) {
	if h.payloadLength > maxControlFramePayload {
		c.Close(StatusProtocolError, "control frame too large")
		return
	}

	if !h.fin {
		c.Close(StatusProtocolError, "control frame cannot be fragmented")
		return
	}

	b := make([]byte, h.payloadLength)
	_, err := io.ReadFull(c.br, b)
	if err != nil {
		c.close(xerrors.Errorf("failed to read control frame payload: %w", err))
		return
	}

	if h.masked {
		fastXOR(h.maskKey, 0, b)
	}

	switch h.opcode {
	case opPing:
		c.writePong(b)
	case opPong:
	case opClose:
		ce, err := parseClosePayload(b)
		if err != nil {
			c.close(xerrors.Errorf("received invalid close payload: %w", err))
			return
		}
		if ce.Code == StatusNoStatusRcvd {
			c.writeClose(nil, ce)
		} else {
			c.Close(ce.Code, ce.Reason)
		}
	default:
		panic(fmt.Sprintf("websocket: unexpected control opcode: %#v", h))
	}
}

func (c *Conn) readLoop() {
	defer close(c.readDone)

	for {
		h, err := readHeader(c.br)
		if err != nil {
			c.close(xerrors.Errorf("failed to read header: %w", err))
			return
		}

		if h.rsv1 || h.rsv2 || h.rsv3 {
			c.Close(StatusProtocolError, fmt.Sprintf("received header with rsv bits set: %v:%v:%v", h.rsv1, h.rsv2, h.rsv3))
			return
		}

		if h.opcode.controlOp() {
			c.handleControl(h)
			continue
		}

		switch h.opcode {
		case opBinary, opText:
			if c.inMsg {
				c.Close(StatusProtocolError, "cannot read new data frame when previous frame is not finished")
				return
			}

			select {
			case <-c.closed:
				return
			case c.read <- h.opcode:
				c.inMsg = true
			}
		case opContinuation:
			if !c.inMsg {
				c.Close(StatusProtocolError, "continuation frame not after data or text frame")
				return
			}
		default:
			c.Close(StatusProtocolError, fmt.Sprintf("unknown opcode %v", h.opcode))
			return
		}

		err = c.dataReadLoop(h)
		if err != nil {
			c.close(xerrors.Errorf("failed to read from connection: %w", err))
			return
		}
	}
}

func (c *Conn) dataReadLoop(h header) error {
	maskPos := 0
	left := h.payloadLength
	firstReadDone := false
	for left > 0 || !firstReadDone {
		select {
		case <-c.closed:
			return c.closeErr
		case b := <-c.readBytes:
			if int64(len(b)) > left {
				b = b[:left]
			}

			_, err := io.ReadFull(c.br, b)
			if err != nil {
				return xerrors.Errorf("failed to read from connection: %w", err)
			}
			left -= int64(len(b))

			if h.masked {
				maskPos = fastXOR(h.maskKey, maskPos, b)
			}

			// Must set this before we signal the read is done.
			// The reader will use this to return io.EOF and
			// c.Read will use it to check if the reader has been completed.
			if left == 0 && h.fin {
				atomic.StoreInt64(&c.activeReader, 0)
				c.inMsg = false
			}

			select {
			case <-c.closed:
				return c.closeErr
			case c.readDone <- len(b):
				firstReadDone = true
			}
		}
	}

	return nil
}

func (c *Conn) writePong(p []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	err := c.writeSingleFrame(ctx, opPong, p)
	return err
}

// Close closes the WebSocket connection with the given status code and reason.
// It will write a WebSocket close frame with a timeout of 5 seconds.
// The connection can only be closed once. Additional calls to Close
// are no-ops.
// The maximum length of reason must be 125 bytes otherwise an internal
// error will be sent to the peer. For this reason, you should avoid
// sending a dynamic reason.
// Close will unblock all goroutines interacting with the connection.
func (c *Conn) Close(code StatusCode, reason string) error {
	err := c.exportedClose(code, reason)
	if err != nil {
		return xerrors.Errorf("failed to close connection: %w", err)
	}
	return nil
}

func (c *Conn) exportedClose(code StatusCode, reason string) error {
	ce := CloseError{
		Code:   code,
		Reason: reason,
	}

	// This function also will not wait for a close frame from the peer like the RFC
	// wants because that makes no sense and I don't think anyone actually follows that.
	// Definitely worth seeing what popular browsers do later.
	p, err := ce.bytes()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal close frame: %v\n", err)
		ce = CloseError{
			Code: StatusInternalError,
		}
		p, _ = ce.bytes()
	}

	return c.writeClose(p, ce)
}

func (c *Conn) writeClose(p []byte, cerr CloseError) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	err := c.writeSingleFrame(ctx, opClose, p)

	c.close(cerr)

	if err != nil {
		return err
	}

	if !xerrors.Is(c.closeErr, cerr) {
		return c.closeErr
	}

	return nil
}

func (c *Conn) writeSingleFrame(ctx context.Context, opcode opcode, p []byte) error {
	ch := c.fastWrite
	if opcode.controlOp() {
		ch = c.control
	}
	select {
	case <-c.closed:
		return c.closeErr
	case ch <- frame{
		opcode:  opcode,
		payload: p,
	}:
	case <-ctx.Done():
		c.close(xerrors.Errorf("control frame write timed out: %w", ctx.Err()))
		return ctx.Err()
	}

	select {
	case <-c.closed:
		return c.closeErr
	case <-c.writeDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Writer returns a writer bounded by the context that will write
// a WebSocket message of type dataType to the connection.
//
// Ensure you close the writer once you have written the entire message.
// Concurrent calls to Writer are ok.
// Only one writer can be open at a time so Writer will block if there is
// another goroutine with an open writer until that writer is closed.
func (c *Conn) Writer(ctx context.Context, typ MessageType) (io.WriteCloser, error) {
	wc, err := c.writer(ctx, typ)
	if err != nil {
		return nil, xerrors.Errorf("failed to get writer: %w", err)
	}
	return wc, nil
}

func (c *Conn) writer(ctx context.Context, typ MessageType) (io.WriteCloser, error) {
	select {
	case <-c.closed:
		return nil, c.closeErr
	case <-ctx.Done():
		return nil, ctx.Err()
	case c.write <- typ:
		return messageWriter{
			ctx: ctx,
			c:   c,
		}, nil
	}
}

// messageWriter enables writing to a WebSocket connection.
type messageWriter struct {
	ctx context.Context
	c   *Conn
}

// Write writes the given bytes to the WebSocket connection.
func (w messageWriter) Write(p []byte) (int, error) {
	n, err := w.write(p)
	if err != nil {
		return n, xerrors.Errorf("failed to write: %w", err)
	}
	return n, nil
}

func (w messageWriter) write(p []byte) (int, error) {
	select {
	case <-w.c.closed:
		return 0, w.c.closeErr
	case w.c.writeBytes <- p:
		select {
		case <-w.ctx.Done():
			w.c.close(xerrors.Errorf("data write timed out: %w", w.ctx.Err()))
			// Wait for writeLoop to complete so we know p is done with.
			<-w.c.writeDone
			return 0, w.ctx.Err()
		case _, ok := <-w.c.writeDone:
			if !ok {
				return 0, w.c.closeErr
			}
			return len(p), nil
		}
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	}
}

// Close flushes the frame to the connection.
// This must be called for every messageWriter.
func (w messageWriter) Close() error {
	err := w.close()
	if err != nil {
		return xerrors.Errorf("failed to close writer: %w", err)
	}
	return nil
}

func (w messageWriter) close() error {
	select {
	case <-w.c.closed:
		return w.c.closeErr
	case <-w.ctx.Done():
		return w.ctx.Err()
	case w.c.writeFlush <- struct{}{}:
		return nil
	}
}

// Reader will wait until there is a WebSocket data message to read from the connection.
// It returns the type of the message and a reader to read it.
// The passed context will also bound the reader.
//
// Your application must keep reading messages for the Conn to automatically respond to ping
// and close frames and not become stuck waiting for a data message to be read.
// Please ensure to read the full message from io.Reader. If you do not read till
// io.EOF, the connection will break unless the next read would have yielded io.EOF.
//
// You can only read a single message at a time so do not call this method
// concurrently.
func (c *Conn) Reader(ctx context.Context) (MessageType, io.Reader, error) {
	typ, r, err := c.reader(ctx)
	if err != nil {
		return 0, nil, xerrors.Errorf("failed to get reader: %w", err)
	}
	return typ, r, nil
}

func (c *Conn) reader(ctx context.Context) (MessageType, io.Reader, error) {
	if !atomic.CompareAndSwapInt64(&c.activeReader, 0, 1) {
		// If the next read yields io.EOF we are good to go.
		r := messageReader{
			ctx: ctx,
			c:   c,
		}
		_, err := r.Read(nil)
		if err == nil {
			return 0, nil, xerrors.New("previous message not fully read")
		}
		if !xerrors.Is(err, io.EOF) {
			return 0, nil, xerrors.Errorf("failed to check if last message at io.EOF: %w", err)
		}

		atomic.StoreInt64(&c.activeReader, 1)
	}

	select {
	case <-c.closed:
		return 0, nil, c.closeErr
	case opcode := <-c.read:
		return MessageType(opcode), messageReader{
			ctx: ctx,
			c:   c,
		}, nil
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

// messageReader enables reading a data frame from the WebSocket connection.
type messageReader struct {
	ctx context.Context
	c   *Conn
}

// Read reads as many bytes as possible into p.
func (r messageReader) Read(p []byte) (int, error) {
	n, err := r.read(p)
	if err != nil {
		// Have to return io.EOF directly for now, we cannot wrap as xerrors
		// isn't used in stdlib.
		if err == io.EOF {
			return n, io.EOF
		}
		return n, xerrors.Errorf("failed to read: %w", err)
	}
	return n, nil
}

func (r messageReader) read(p []byte) (_ int, err error) {
	if atomic.LoadInt64(&r.c.activeReader) == 0 {
		return 0, io.EOF
	}

	select {
	case <-r.c.closed:
		return 0, r.c.closeErr
	case r.c.readBytes <- p:
		select {
		case <-r.ctx.Done():
			r.c.close(xerrors.Errorf("data read timed out: %w", r.ctx.Err()))
			// Wait for readLoop to complete so we know p is done.
			<-r.c.readDone
			return 0, r.ctx.Err()
		case n, ok := <-r.c.readDone:
			if !ok {
				return 0, r.c.closeErr
			}
			if atomic.LoadInt64(&r.c.activeReader) == 0 {
				return n, io.EOF
			}
			return n, nil
		}
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	}
}
