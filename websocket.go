package websocket

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"time"

	"golang.org/x/xerrors"
)

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

	writeDataLock  chan struct{}
	writeFrameLock chan struct{}

	readData chan header
	readDone chan struct{}

	setReadTimeout  chan context.Context
	setWriteTimeout chan context.Context
	setConnContext  chan context.Context
	getConnContext  chan context.Context
}

// Context returns a context derived from parent that will be cancelled
// when the connection is closed.
// If the parent context is cancelled, the connection will be closed.
//
// This is an experimental API meaning it may be remove in the future.
// Please let me know how you feel about it.
func (c *Conn) Context(parent context.Context) context.Context {
	select {
	case <-c.closed:
		ctx, cancel := context.WithCancel(parent)
		cancel()
		return ctx
	case c.setConnContext <- parent:
	}

	select {
	case <-c.closed:
		ctx, cancel := context.WithCancel(parent)
		cancel()
		return ctx
	case ctx := <-c.getConnContext:
		return ctx
	}
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

	c.writeDataLock = make(chan struct{}, 1)
	c.writeFrameLock = make(chan struct{}, 1)

	c.readData = make(chan header)
	c.readDone = make(chan struct{})

	c.setReadTimeout = make(chan context.Context)
	c.setWriteTimeout = make(chan context.Context)
	c.setConnContext = make(chan context.Context)
	c.getConnContext = make(chan context.Context)

	runtime.SetFinalizer(c, func(c *Conn) {
		c.close(xerrors.New("connection garbage collected"))
	})

	go c.timeoutLoop()
	go c.readLoop()
}

// We never mask inside here because our mask key is always 0,0,0,0.
// See comment on secWebSocketKey.
func (c *Conn) writeFrame(ctx context.Context, h header, p []byte) error {
	err := c.acquireLock(ctx, c.writeFrameLock)
	if err != nil {
		return err
	}
	defer c.releaseLock(c.writeFrameLock)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return c.closeErr
	case c.setWriteTimeout <- ctx:
	}
	defer func() {
		// We have to remove the write timeout, even if ctx is cancelled.
		select {
		case <-c.closed:
			return
		case c.setWriteTimeout <- context.Background():
		}
	}()

	h.masked = c.client
	h.payloadLength = int64(len(p))

	b2 := marshalHeader(h)
	_, err = c.bw.Write(b2)
	if err != nil {
		c.close(xerrors.Errorf("failed to write to connection: %w", err))
		return c.closeErr
	}
	_, err = c.bw.Write(p)
	if err != nil {
		c.close(xerrors.Errorf("failed to write to connection: %w", err))
		return c.closeErr

	}

	if h.fin {
		err := c.bw.Flush()
		if err != nil {
			c.close(xerrors.Errorf("failed to write to connection: %w", err))
			return c.closeErr
		}
	}

	return nil
}

func (c *Conn) timeoutLoop() {
	readCtx := context.Background()
	writeCtx := context.Background()
	parentCtx := context.Background()
	cancelCtx := func() {}
	defer func() {
		// We do not defer cancelCtx because its value can change.
		cancelCtx()
	}()

	for {
		select {
		case <-c.closed:
			return
		case readCtx = <-c.setWriteTimeout:
		case writeCtx = <-c.setReadTimeout:
		case <-readCtx.Done():
			c.close(xerrors.Errorf("data read timed out: %w", readCtx.Err()))
		case <-writeCtx.Done():
			c.close(xerrors.Errorf("data write timed out: %w", writeCtx.Err()))
		case <-parentCtx.Done():
			c.close(xerrors.Errorf("parent context cancelled: %w", parentCtx.Err()))
			return
		case parentCtx = <-c.setConnContext:
			var ctx context.Context
			ctx, cancelCtx = context.WithCancel(parentCtx)
			select {
			case <-c.closed:
				return
			case <-parentCtx.Done():
				c.close(xerrors.Errorf("parent context cancelled: %w", parentCtx.Err()))
				return
			case c.getConnContext <- ctx:
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

func (c *Conn) readTillData() (header, error) {
	for {
		h, err := readHeader(c.br)
		if err != nil {
			return header{}, xerrors.Errorf("failed to read header: %w", err)
		}

		if h.rsv1 || h.rsv2 || h.rsv3 {
			ce := CloseError{
				Code:   StatusProtocolError,
				Reason: fmt.Sprintf("received header with rsv bits set: %v:%v:%v", h.rsv1, h.rsv2, h.rsv3),
			}
			c.Close(ce.Code, ce.Reason)
			return header{}, ce
		}

		if h.opcode.controlOp() {
			c.handleControl(h)
			continue
		}

		switch h.opcode {
		case opBinary, opText, opContinuation:
			return h, nil
		default:
			ce := CloseError{
				Code:   StatusProtocolError,
				Reason: fmt.Sprintf("unknown opcode %v", h.opcode),
			}
			c.Close(ce.Code, ce.Reason)
			return header{}, ce
		}
	}
}

func (c *Conn) readLoop() {
	for {
		h, err := c.readTillData()
		if err != nil {
			c.close(err)
			return
		}

		select {
		case <-c.closed:
			return
		case c.readData <- h:
		}

		select {
		case <-c.closed:
			return
		case <-c.readDone:
		}
	}
}

func (c *Conn) writePong(p []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	err := c.writeCompleteMessage(ctx, opPong, p)
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

	err := c.writeCompleteMessage(ctx, opClose, p)

	c.close(cerr)

	if err != nil {
		return err
	}

	if !xerrors.Is(c.closeErr, cerr) {
		return c.closeErr
	}

	return nil
}

func (c *Conn) acquireLock(ctx context.Context, lock chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return c.closeErr
	case lock <- struct{}{}:
		return nil
	}
}

func (c *Conn) releaseLock(lock chan struct{}) {
	<-lock
}

func (c *Conn) writeCompleteMessage(ctx context.Context, opcode opcode, p []byte) error {
	if !opcode.controlOp() {
		err := c.acquireLock(ctx, c.writeDataLock)
		if err != nil {
			return err
		}
		defer c.releaseLock(c.writeDataLock)
	}

	err := c.writeFrame(ctx, header{
		fin:    true,
		opcode: opcode,
	}, p)
	if err != nil {
		return xerrors.Errorf("failed to write frame: %v", err)
	}
	return nil
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
	err := c.acquireLock(ctx, c.writeDataLock)
	if err != nil {
		return nil, err
	}
	return &messageWriter{
		ctx:    ctx,
		opcode: opcode(typ),
		c:      c,
	}, nil
}

// messageWriter enables writing to a WebSocket connection.
type messageWriter struct {
	ctx    context.Context
	opcode opcode
	c      *Conn
	closed bool
}

// Write writes the given bytes to the WebSocket connection.
func (w *messageWriter) Write(p []byte) (int, error) {
	n, err := w.write(p)
	if err != nil {
		return n, xerrors.Errorf("failed to write: %w", err)
	}
	return n, nil
}

func (w *messageWriter) write(p []byte) (int, error) {
	if w.closed {
		return 0, xerrors.Errorf("cannot use closed writer")
	}
	err := w.c.writeFrame(w.ctx, header{
		opcode: w.opcode,
	}, p)
	if err != nil {
		return 0, err
	}
	w.opcode = opContinuation
	return len(p), nil
}

// Close flushes the frame to the connection.
// This must be called for every messageWriter.
func (w *messageWriter) Close() error {
	err := w.close()
	if err != nil {
		return xerrors.Errorf("failed to close writer: %w", err)
	}
	return nil
}

func (w *messageWriter) close() error {
	if w.closed {
		return xerrors.Errorf("cannot use closed writer")
	}
	w.closed = true

	err := w.c.writeFrame(w.ctx, header{
		fin:    true,
		opcode: w.opcode,
	}, nil)
	if err != nil {
		return err
	}

	w.c.releaseLock(w.c.writeDataLock)
	return nil
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
	// if !atomic.CompareAndSwapInt64(&c.activeReader, 0, 1) {
	// 	// If the next read yields io.EOF we are good to go.
	// 	r := messageReader{
	// 		ctx: ctx,
	// 		c:   c,
	// 	}
	// 	_, err := r.Read(nil)
	// 	if err == nil {
	// 		return 0, nil, xerrors.New("previous message not fully read")
	// 	}
	// 	if !xerrors.Is(err, io.EOF) {
	// 		return 0, nil, xerrors.Errorf("failed to check if last message at io.EOF: %w", err)
	// 	}
	//
	// 	atomic.StoreInt64(&c.activeReader, 1)
	// }

	select {
	case <-c.closed:
		return 0, nil, c.closeErr
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case c.setReadTimeout <- ctx:
	}

	select {
	case <-c.closed:
		return 0, nil, c.closeErr
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case h := <-c.readData:
		if h.opcode == opContinuation {
			if h.fin && h.payloadLength == 0 {
				select {
				case <-c.closed:
					return 0, nil, c.closeErr
				case c.readDone <- struct{}{}:
					return c.reader(ctx)
				}
			}
			return 0, nil, xerrors.Errorf("previous reader was not read to EOF")
		}
		return MessageType(h.opcode), &messageReader{
			h: &h,
			c: c,
		}, nil
	}
}

// messageReader enables reading a data frame from the WebSocket connection.
type messageReader struct {
	maskPos int
	h       *header
	c       *Conn
	eofed   bool
}

// Read reads as many bytes as possible into p.
func (r *messageReader) Read(p []byte) (int, error) {
	n, err := r.read(p)
	if err != nil {
		// Have to return io.EOF directly for now, we cannot wrap as xerrors
		// isn't used in stdlib.
		if xerrors.Is(err, io.EOF) {
			return n, io.EOF
		}
		return n, xerrors.Errorf("failed to read: %w", err)
	}
	return n, nil
}

func (r *messageReader) read(p []byte) (int, error) {
	if r.eofed {
		return 0, xerrors.Errorf("cannot use EOFed reader")
	}

	if r.h == nil {
		select {
		case <-r.c.closed:
			return 0, r.c.closeErr
		case h := <-r.c.readData:
			if h.opcode != opContinuation {
				ce := CloseError{
					Code:   StatusProtocolError,
					Reason: "cannot read new data frame when previous frame is not finished",
				}
				r.c.Close(ce.Code, ce.Reason)
				return 0, ce
			}
			r.h = &h
		}
	}

	if int64(len(p)) > r.h.payloadLength {
		p = p[:r.h.payloadLength]
	}

	n, err := io.ReadFull(r.c.br, p)

	r.h.payloadLength -= int64(n)
	if r.h.masked {
		r.maskPos = fastXOR(r.h.maskKey, r.maskPos, p)
	}

	if err != nil {
		r.c.close(xerrors.Errorf("failed to read control frame payload: %w", err))
		return n, r.c.closeErr
	}

	if r.h.payloadLength == 0 {
		select {
		case <-r.c.closed:
			return n, r.c.closeErr
		case r.c.readDone <- struct{}{}:
		}
		if r.h.fin {
			r.eofed = true
			select {
			case <-r.c.closed:
				return n, r.c.closeErr
			case r.c.setReadTimeout <- context.Background():
				return n, io.EOF
			}
		}
		r.maskPos = 0
		r.h = nil
	}

	return n, nil
}
