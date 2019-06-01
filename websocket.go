package websocket

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/xerrors"
)

// Conn represents a WebSocket connection.
// All methods may be called concurrently.
//
// Please be sure to call Close on the connection when you
// are finished with it to release the associated resources.
type Conn struct {
	subprotocol string
	br          *bufio.Reader
	bw          *bufio.Writer
	closer      io.Closer
	client      bool

	// In bytes.
	msgReadLimit int64

	closeOnce sync.Once
	closeErr  error
	closed    chan struct{}

	// writeMsgLock is acquired to write a multi frame message.
	writeMsgLock   chan struct{}
	// writeFrameLock is acquired to write a single frame.
	// Effectively meaning whoever holds it gets to write to bw.
	writeFrameLock chan struct{}

	// readMsgLock is acquired to read a message with Reader.
	readMsgLock   chan struct{}
	// readFrameLock is acquired to read from bw.
	readFrameLock chan struct{}
	// readMsg is used by messageReader to receive frames from
	// readLoop.
	readMsg       chan header
	// readMsgDone is used to tell the readLoop to continue after
	// messageReader has read a frame.
	readMsgDone   chan struct{}

	setReadTimeout  chan context.Context
	setWriteTimeout chan context.Context
	setConnContext  chan context.Context
	getConnContext  chan context.Context

	activePingsMu sync.Mutex
	activePings   map[string]chan<- struct{}
}

// Context returns a context derived from parent that will be cancelled
// when the connection is closed or broken.
// If the parent context is cancelled, the connection will be closed.
//
// This is an experimental API.
// Please let me know how you feel about it in https://github.com/nhooyr/websocket/issues/79
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

		c.closeErr = xerrors.Errorf("websocket closed: %w", err)
		close(c.closed)

		// Have to close after c.closed is closed to ensure any goroutine that wakes up
		// from the connection being closed also sees that c.closed is closed and returns
		// closeErr.
		c.closer.Close()

		// See comment in dial.go
		if c.client {
			// By acquiring the locks, we ensure no goroutine will touch the bufio reader or writer
			// and we can safely return them.
			// Whenever a caller holds this lock and calls close, it ensures to release the lock to prevent
			// a deadlock.
			// As of now, this is in writeFrame, readPayload and readHeader.
			c.readFrameLock <- struct{}{}
			returnBufioReader(c.br)

			c.writeFrameLock <- struct{}{}
			returnBufioWriter(c.bw)
		}
	})
}

// Subprotocol returns the negotiated subprotocol.
// An empty string means the default protocol.
func (c *Conn) Subprotocol() string {
	return c.subprotocol
}

func (c *Conn) init() {
	c.closed = make(chan struct{})

	c.msgReadLimit = 32768

	c.writeMsgLock = make(chan struct{}, 1)
	c.writeFrameLock = make(chan struct{}, 1)

	c.readMsgLock = make(chan struct{}, 1)
	c.readFrameLock = make(chan struct{}, 1)
	c.readMsg = make(chan header)
	c.readMsgDone = make(chan struct{})

	c.setReadTimeout = make(chan context.Context)
	c.setWriteTimeout = make(chan context.Context)
	c.setConnContext = make(chan context.Context)
	c.getConnContext = make(chan context.Context)

	c.activePings = make(map[string]chan<- struct{})

	runtime.SetFinalizer(c, func(c *Conn) {
		c.close(xerrors.New("connection garbage collected"))
	})

	go c.timeoutLoop()
	go c.readLoop()
}

func (c *Conn) writeControl(ctx context.Context, opcode opcode, p []byte) error {
	err := c.writeFrame(ctx, true, opcode, p)
	if err != nil {
		return xerrors.Errorf("failed to write control frame: %w", err)
	}
	return nil
}

// writeFrame handles all writes to the connection.
// We never mask inside here because our mask key is always 0,0,0,0.
// See comment on secWebSocketKey for why.
func (c *Conn) writeFrame(ctx context.Context, fin bool, opcode opcode, p []byte) error {
	h := header{
		fin:           fin,
		opcode:        opcode,
		masked:        c.client,
		payloadLength: int64(len(p)),
	}
	b2 := marshalHeader(h)

	err := c.acquireLock(ctx, c.writeFrameLock)
	if err != nil {
		return err
	}
	defer c.releaseLock(c.writeFrameLock)

	select {
	case <-c.closed:
		return c.closeErr
	case c.setWriteTimeout <- ctx:
	}

	writeErr := func(err error) error {
		select {
		case <-c.closed:
			return c.closeErr
		default:
		}

		err = xerrors.Errorf("failed to write to connection: %w", err)
		// We need to release the lock first before closing the connection to ensure
		// the lock can be acquired inside close to ensure no one can access c.bw.
		c.releaseLock(c.writeFrameLock)
		c.close(err)

		return err
	}

	_, err = c.bw.Write(b2)
	if err != nil {
		return writeErr(err)
	}
	_, err = c.bw.Write(p)
	if err != nil {
		return writeErr(err)
	}

	if fin {
		err = c.bw.Flush()
		if err != nil {
			return writeErr(err)
		}
	}

	// We already finished writing, no need to potentially brick the connection if
	// the context expires.
	select {
	case <-c.closed:
		return c.closeErr
	case c.setWriteTimeout <- context.Background():
	}

	return nil
}

func (c *Conn) timeoutLoop() {
	readCtx := context.Background()
	writeCtx := context.Background()
	parentCtx := context.Background()

	for {
		select {
		case <-c.closed:
			return
		case writeCtx = <-c.setWriteTimeout:
		case readCtx = <-c.setReadTimeout:
		case <-readCtx.Done():
			c.close(xerrors.Errorf("data read timed out: %w", readCtx.Err()))
		case <-writeCtx.Done():
			c.close(xerrors.Errorf("data write timed out: %w", writeCtx.Err()))
		case <-parentCtx.Done():
			c.close(xerrors.Errorf("parent context cancelled: %w", parentCtx.Err()))
			return
		case parentCtx = <-c.setConnContext:
			ctx, cancelCtx := context.WithCancel(parentCtx)
			defer cancelCtx()

			select {
			case <-c.closed:
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

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	b := make([]byte, h.payloadLength)

	_, err := c.readPayload(ctx, b)
	if err != nil {
		return
	}

	if h.masked {
		fastXOR(h.maskKey, 0, b)
	}

	switch h.opcode {
	case opPing:
		c.writePong(b)
	case opPong:
		c.activePingsMu.Lock()
		pong, ok := c.activePings[string(b)]
		c.activePingsMu.Unlock()
		if ok {
			close(pong)
		}
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

func (c *Conn) readTillMsg() (header, error) {
	for {
		h, err := c.readHeader()
		if err != nil {
			return header{}, err
		}

		if h.rsv1 || h.rsv2 || h.rsv3 {
			err := xerrors.Errorf("received header with rsv bits set: %v:%v:%v", h.rsv1, h.rsv2, h.rsv3)
			c.Close(StatusProtocolError, err.Error())
			return header{}, err
		}

		if h.opcode.controlOp() {
			c.handleControl(h)
			continue
		}

		switch h.opcode {
		case opBinary, opText, opContinuation:
			return h, nil
		default:
			err := xerrors.Errorf("received unknown opcode %v", h.opcode)
			c.Close(StatusProtocolError, err.Error())
			return header{}, err
		}
	}
}

func (c *Conn) readHeader() (header, error) {
	err := c.acquireLock(context.Background(), c.readFrameLock)
	if err != nil {
		return header{}, err
	}
	defer c.releaseLock(c.readFrameLock)

	h, err := readHeader(c.br)
	if err != nil {
		err := xerrors.Errorf("failed to read header: %w", err)
		c.releaseLock(c.readFrameLock)
		c.close(err)
		return header{}, err
	}

	return h, nil
}

func (c *Conn) readLoop() {
	for {
		h, err := c.readTillMsg()
		if err != nil {
			return
		}

		select {
		case <-c.closed:
			return
		case c.readMsg <- h:
		}

		select {
		case <-c.closed:
			return
		case <-c.readMsgDone:
		}
	}
}

func (c *Conn) writePong(p []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	err := c.writeControl(ctx, opPong, p)
	return err
}

// Close closes the WebSocket connection with the given status code and reason.
//
// It will write a WebSocket close frame with a timeout of 5 seconds.
// The connection can only be closed once. Additional calls to Close
// are no-ops.
//
// The maximum length of reason must be 125 bytes otherwise an internal
// error will be sent to the peer. For this reason, you should avoid
// sending a dynamic reason.
//
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
		fmt.Fprintf(os.Stderr, "websocket: failed to marshal close frame: %v\n", err)
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

	err := c.writeControl(ctx, opClose, p)

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
	// Allow multiple releases.
	select {
	case <-lock:
	default:
	}
}

// Writer returns a writer bounded by the context that will write
// a WebSocket message of type dataType to the connection.
//
// You must close the writer once you have written the entire message.
//
// Only one writer can be open at a time, multiple calls will block until the previous writer
// is closed.
func (c *Conn) Writer(ctx context.Context, typ MessageType) (io.WriteCloser, error) {
	wc, err := c.writer(ctx, typ)
	if err != nil {
		return nil, xerrors.Errorf("failed to get writer: %w", err)
	}
	return wc, nil
}

func (c *Conn) writer(ctx context.Context, typ MessageType) (io.WriteCloser, error) {
	err := c.acquireLock(ctx, c.writeMsgLock)
	if err != nil {
		return nil, err
	}
	return &messageWriter{
		ctx:    ctx,
		opcode: opcode(typ),
		c:      c,
	}, nil
}

// Read is a convenience method to read a single message from the connection.
//
// See the Reader method if you want to be able to reuse buffers or want to stream a message.
// The docs on Reader apply to this metohd as well.
//
// This is an experimental API, please let me know how you feel about it in
// https://github.com/nhooyr/websocket/issues/62
func (c *Conn) Read(ctx context.Context) (MessageType, []byte, error) {
	typ, r, err := c.Reader(ctx)
	if err != nil {
		return 0, nil, err
	}

	b, err := ioutil.ReadAll(r)
	if err != nil {
		return typ, b, err
	}

	return typ, b, nil
}

// Write is a convenience method to write a message to the connection.
//
// See the Writer method if you want to stream a message. The docs on Writer
// regarding concurrency also apply to this method.
//
// This is an experimental API, please let me know how you feel about it in
// https://github.com/nhooyr/websocket/issues/62
func (c *Conn) Write(ctx context.Context, typ MessageType, p []byte) error {
	err := c.write(ctx, typ, p)
	if err != nil {
		return xerrors.Errorf("failed to write msg: %w", err)
	}
	return nil
}

func (c *Conn) write(ctx context.Context, typ MessageType, p []byte) error {
	err := c.acquireLock(ctx, c.writeMsgLock)
	if err != nil {
		return err
	}
	defer c.releaseLock(c.writeMsgLock)

	err = c.writeFrame(ctx, true, opcode(typ), p)
	if err != nil {
		return err
	}
	return nil
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
	err := w.c.writeFrame(w.ctx, false, w.opcode, p)
	if err != nil {
		return 0, xerrors.Errorf("failed to write data frame: %w", err)
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

	err := w.c.writeFrame(w.ctx, true, w.opcode, nil)
	if err != nil {
		return xerrors.Errorf("failed to write fin frame: %w", err)
	}

	w.c.releaseLock(w.c.writeMsgLock)
	return nil
}

// Reader will wait until there is a WebSocket data message to read from the connection.
// It returns the type of the message and a reader to read it.
// The passed context will also bound the reader.
//
// Control (ping, pong, close) frames will be handled automatically
// in a separate goroutine so if you do not expect any data messages,
// you do not need  to read from the connection. However, if the peer
// sends a data message, further pings, pongs and close frames will not
// be read if you do not read the message from the connection.
//
// If you do not read from the reader till EOF, nothing further will be read from the connection.
// Only one reader can be open at a time, multiple calls will block until the previous reader
// is read to completion.
// TODO remove concurrent reads.
func (c *Conn) Reader(ctx context.Context) (MessageType, io.Reader, error) {
	// We could handle the case of json.Decoder where the message may not be read
	// till EOF but would still be read till the end of data. E.g. if the other side
	// sends a fin frame after the message, we could allow the code to continue and
	// just pick off but the code for that gets complicated and if there is real data
	// after the JSON object, Reader would block until the timeout is hit
	typ, r, err := c.reader(ctx)
	if err != nil {
		return 0, nil, xerrors.Errorf("failed to get reader: %w", err)
	}
	return typ, &limitedReader{
		c:    c,
		r:    r,
		left: atomic.LoadInt64(&c.msgReadLimit),
	}, nil
}

func (c *Conn) reader(ctx context.Context) (_ MessageType, _ io.Reader, err error) {
	err = c.acquireLock(ctx, c.readMsgLock)
	if err != nil {
		return 0, nil, err
	}

	select {
	case <-c.closed:
		return 0, nil, c.closeErr
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case h := <-c.readMsg:
		if h.opcode == opContinuation {
			err := xerrors.Errorf("received continuation frame not after data or text frame")
			c.Close(StatusProtocolError, err.Error())
			return 0, nil, err
		}
		return MessageType(h.opcode), &messageReader{
			ctx: ctx,
			h:   &h,
			c:   c,
		}, nil
	}
}

// messageReader enables reading a data frame from the WebSocket connection.
type messageReader struct {
	ctx     context.Context
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
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		case h := <-r.c.readMsg:
			if h.opcode != opContinuation {
				err := xerrors.Errorf("received new data frame without finishing the previous frame")
				r.c.Close(StatusProtocolError, err.Error())
				return 0, err
			}
			r.h = &h
		}
	}

	if int64(len(p)) > r.h.payloadLength {
		p = p[:r.h.payloadLength]
	}

	n, err := r.readPayload(p)

	r.h.payloadLength -= int64(n)
	if r.h.masked {
		r.maskPos = fastXOR(r.h.maskKey, r.maskPos, p)
	}

	if err != nil {
		err := xerrors.Errorf("failed to read frame payload: %w", err)
		r.c.close(err)
		return n, err
	}

	if r.h.payloadLength == 0 {
		select {
		case <-r.c.closed:
			return n, r.c.closeErr
		case r.c.readMsgDone <- struct{}{}:
		}

		if r.h.fin {
			r.eofed = true
			r.c.releaseLock(r.c.readMsgLock)
			return n, io.EOF
		}

		r.maskPos = 0
		r.h = nil
	}

	return n, nil
}

func (c *Conn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func (c *Conn) readPayload(ctx context.Context, p []byte) (int, error) {
	err := c.acquireLock(ctx, c.readFrameLock)
	if err != nil {
		return 0, err
	}
	defer c.releaseLock(c.readFrameLock)

	select {
	case <-c.closed:
		return 0, c.closeErr
	case c.setReadTimeout <- ctx:
	}

	n, err := io.ReadFull(c.br, p)
	if err != nil {
		select {
		case <-c.closed:
			return n, c.closeErr
		default:
		}
		err = xerrors.Errorf("failed to read from connection: %w", err)
		c.releaseLock(c.readFrameLock)
		c.close(err)
		return n, err
	}

	select {
	case <-c.closed:
		return 0, c.closeErr
	case c.setReadTimeout <- context.Background():
	}

	return 0, err
}

// SetReadLimit sets the max number of bytes to read for a single message.
// It applies to the Reader and Read methods.
//
// By default, the connection has a message read limit of 32768 bytes.
//
// When the limit is hit, the connection will be closed with StatusPolicyViolation.
func (c *Conn) SetReadLimit(n int64) {
	atomic.StoreInt64(&c.msgReadLimit, n)
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

// Ping sends a ping to the peer and waits for a pong.
// Use this to measure latency or ensure the peer is responsive.
//
// This API is experimental.
// Please provide feedback in https://github.com/nhooyr/websocket/issues/1.
func (c *Conn) Ping(ctx context.Context) error {
	err := c.ping(ctx)
	if err != nil {
		return xerrors.Errorf("failed to ping: %w", err)
	}
	return nil
}

func (c *Conn) ping(ctx context.Context) error {
	id := rand.Uint64()
	p := strconv.FormatUint(id, 10)

	pong := make(chan struct{})

	c.activePingsMu.Lock()
	c.activePings[p] = pong
	c.activePingsMu.Unlock()

	defer func() {
		c.activePingsMu.Lock()
		delete(c.activePings, p)
		c.activePingsMu.Unlock()
	}()

	err := c.writeControl(ctx, opPing, []byte(p))
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return c.closeErr
	case <-pong:
		return nil
	}
}
