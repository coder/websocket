// +build !js

package websocket

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket/internal/bpool"
)

// Conn represents a WebSocket connection.
// All methods may be called concurrently except for Reader and Read.
//
// You must always read from the connection. Otherwise control
// frames will not be handled. See the docs on Reader and CloseRead.
//
// Be sure to call Close on the connection when you
// are finished with it to release the associated resources.
//
// Every error from Read or Reader will cause the connection
// to be closed so you do not need to write your own error message.
// This applies to the Read methods in the wsjson/wspb subpackages as well.
type Conn struct {
	subprotocol string
	br          *bufio.Reader
	bw          *bufio.Writer
	// writeBuf is used for masking, its the buffer in bufio.Writer.
	// Only used by the client for masking the bytes in the buffer.
	writeBuf []byte
	closer   io.Closer
	client   bool

	closeOnce     sync.Once
	closeErrOnce  sync.Once
	closeErr      error
	closed        chan struct{}
	closing       *atomicInt64
	closeReceived error

	// messageWriter state.
	// writeMsgLock is acquired to write a data message.
	writeMsgLock chan struct{}
	// writeFrameLock is acquired to write a single frame.
	// Effectively meaning whoever holds it gets to write to bw.
	writeFrameLock chan struct{}
	writeHeaderBuf []byte
	writeHeader    *header
	// read limit for a message in bytes.
	msgReadLimit *atomicInt64

	// Used to ensure a previous writer is not used after being closed.
	activeWriter atomic.Value
	// messageWriter state.
	writeMsgOpcode opcode
	writeMsgCtx    context.Context
	readMsgLeft    int64

	// Used to ensure the previous reader is read till EOF before allowing
	// a new one.
	activeReader *messageReader
	// readFrameLock is acquired to read from bw.
	readFrameLock     chan struct{}
	isReadClosed      *atomicInt64
	readHeaderBuf     []byte
	controlPayloadBuf []byte
	readLock          chan struct{}

	// messageReader state.
	readerMsgCtx    context.Context
	readerMsgHeader header
	readerFrameEOF  bool
	readerMaskKey   uint32

	setReadTimeout  chan context.Context
	setWriteTimeout chan context.Context

	pingCounter   *atomicInt64
	activePingsMu sync.Mutex
	activePings   map[string]chan<- struct{}

	logf func(format string, v ...interface{})
}

func (c *Conn) init() {
	c.closed = make(chan struct{})
	c.closing = &atomicInt64{}

	c.msgReadLimit = &atomicInt64{}
	c.msgReadLimit.Store(32768)

	c.writeMsgLock = make(chan struct{}, 1)
	c.writeFrameLock = make(chan struct{}, 1)

	c.readFrameLock = make(chan struct{}, 1)
	c.readLock = make(chan struct{}, 1)

	c.setReadTimeout = make(chan context.Context)
	c.setWriteTimeout = make(chan context.Context)

	c.pingCounter = &atomicInt64{}
	c.activePings = make(map[string]chan<- struct{})

	c.writeHeaderBuf = makeWriteHeaderBuf()
	c.writeHeader = &header{}
	c.readHeaderBuf = makeReadHeaderBuf()
	c.isReadClosed = &atomicInt64{}
	c.controlPayloadBuf = make([]byte, maxControlFramePayload)

	runtime.SetFinalizer(c, func(c *Conn) {
		c.close(errors.New("connection garbage collected"))
	})

	c.logf = log.Printf

	go c.timeoutLoop()
}

// Subprotocol returns the negotiated subprotocol.
// An empty string means the default protocol.
func (c *Conn) Subprotocol() string {
	return c.subprotocol
}

func (c *Conn) close(err error) {
	c.closeOnce.Do(func() {
		runtime.SetFinalizer(c, nil)

		c.setCloseErr(err)
		close(c.closed)

		// Have to close after c.closed is closed to ensure any goroutine that wakes up
		// from the connection being closed also sees that c.closed is closed and returns
		// closeErr.
		c.closer.Close()

		// See comment on bufioReaderPool in handshake.go
		if c.client {
			// By acquiring the locks, we ensure no goroutine will touch the bufio reader or writer
			// and we can safely return them.
			// Whenever a caller holds this lock and calls close, it ensures to release the lock to prevent
			// a deadlock.
			// As of now, this is in writeFrame, readFramePayload and readHeader.
			c.readFrameLock <- struct{}{}
			returnBufioReader(c.br)

			c.writeFrameLock <- struct{}{}
			returnBufioWriter(c.bw)
		}
	})
}

func (c *Conn) timeoutLoop() {
	readCtx := context.Background()
	writeCtx := context.Background()

	for {
		select {
		case <-c.closed:
			return

		case writeCtx = <-c.setWriteTimeout:
		case readCtx = <-c.setReadTimeout:

		case <-readCtx.Done():
			c.setCloseErr(fmt.Errorf("read timed out: %w", readCtx.Err()))
			// Guaranteed to eventually close the connection since we can only ever send
			// one close frame.
			go func() {
				c.exportedClose(StatusPolicyViolation, "read timed out", true)
				// Ensure the connection closes, i.e if we already sent a close frame and timed out
				// to read the peer's close frame.
				c.close(nil)
			}()
			readCtx = context.Background()
		case <-writeCtx.Done():
			c.close(fmt.Errorf("write timed out: %w", writeCtx.Err()))
			return
		}
	}
}

func (c *Conn) acquireLock(ctx context.Context, lock chan struct{}) error {
	select {
	case <-ctx.Done():
		var err error
		switch lock {
		case c.writeFrameLock, c.writeMsgLock:
			err = fmt.Errorf("could not acquire write lock: %v", ctx.Err())
		case c.readFrameLock, c.readLock:
			err = fmt.Errorf("could not acquire read lock: %v", ctx.Err())
		default:
			panic(fmt.Sprintf("websocket: failed to acquire unknown lock: %v", ctx.Err()))
		}
		c.close(err)
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

func (c *Conn) readTillMsg(ctx context.Context) (header, error) {
	for {
		h, err := c.readFrameHeader(ctx)
		if err != nil {
			return header{}, err
		}

		if h.rsv1 || h.rsv2 || h.rsv3 {
			err := fmt.Errorf("received header with rsv bits set: %v:%v:%v", h.rsv1, h.rsv2, h.rsv3)
			c.exportedClose(StatusProtocolError, err.Error(), false)
			return header{}, err
		}

		if h.opcode.controlOp() {
			err = c.handleControl(ctx, h)
			if err != nil {
				// Pass through CloseErrors when receiving a close frame.
				if h.opcode == opClose && CloseStatus(err) != -1 {
					return header{}, err
				}
				return header{}, fmt.Errorf("failed to handle control frame %v: %w", h.opcode, err)
			}
			continue
		}

		switch h.opcode {
		case opBinary, opText, opContinuation:
			return h, nil
		default:
			err := fmt.Errorf("received unknown opcode %v", h.opcode)
			c.exportedClose(StatusProtocolError, err.Error(), false)
			return header{}, err
		}
	}
}

func (c *Conn) readFrameHeader(ctx context.Context) (_ header, err error) {
	wrap := func(err error) error {
		return fmt.Errorf("failed to read frame header: %w", err)
	}
	defer func() {
		if err != nil {
			err = wrap(err)
		}
	}()

	err = c.acquireLock(ctx, c.readFrameLock)
	if err != nil {
		return header{}, err
	}
	defer c.releaseLock(c.readFrameLock)

	select {
	case <-c.closed:
		return header{}, c.closeErr
	case c.setReadTimeout <- ctx:
	}

	h, err := readHeader(c.readHeaderBuf, c.br)
	if err != nil {
		select {
		case <-c.closed:
			return header{}, c.closeErr
		case <-ctx.Done():
			err = ctx.Err()
		default:
		}
		c.releaseLock(c.readFrameLock)
		c.close(wrap(err))
		return header{}, err
	}

	select {
	case <-c.closed:
		return header{}, c.closeErr
	case c.setReadTimeout <- context.Background():
	}

	return h, nil
}

func (c *Conn) handleControl(ctx context.Context, h header) error {
	if h.payloadLength > maxControlFramePayload {
		err := fmt.Errorf("received too big control frame at %v bytes", h.payloadLength)
		c.exportedClose(StatusProtocolError, err.Error(), false)
		return err
	}

	if !h.fin {
		err := errors.New("received fragmented control frame")
		c.exportedClose(StatusProtocolError, err.Error(), false)
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	b := c.controlPayloadBuf[:h.payloadLength]
	_, err := c.readFramePayload(ctx, b)
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
	case opClose:
		ce, err := parseClosePayload(b)
		if err != nil {
			err = fmt.Errorf("received invalid close payload: %w", err)
			c.exportedClose(StatusProtocolError, err.Error(), false)
			c.closeReceived = err
			return err
		}

		err = fmt.Errorf("received close: %w", ce)
		c.closeReceived = err
		c.writeClose(b, err, false)

		if ctx.Err() != nil {
			// The above close probably has been returned by the peer in response
			// to our read timing out so we have to return the read timed out error instead.
			return fmt.Errorf("read timed out: %w", ctx.Err())
		}

		return err
	default:
		panic(fmt.Sprintf("websocket: unexpected control opcode: %#v", h))
	}
}

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
	if c.isReadClosed.Load() == 1 {
		return 0, nil, errors.New("websocket connection read closed")
	}

	typ, r, err := c.reader(ctx, true)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to get reader: %w", err)
	}
	return typ, r, nil
}

func (c *Conn) reader(ctx context.Context, lock bool) (MessageType, io.Reader, error) {
	if lock {
		err := c.acquireLock(ctx, c.readLock)
		if err != nil {
			return 0, nil, err
		}
		defer c.releaseLock(c.readLock)
	}

	if c.activeReader != nil && !c.readerFrameEOF {
		// The only way we know for sure the previous reader is not yet complete is
		// if there is an active frame not yet fully read.
		// Otherwise, a user may have read the last byte but not the EOF if the EOF
		// is in the next frame so we check for that below.
		return 0, nil, errors.New("previous message not read to completion")
	}

	h, err := c.readTillMsg(ctx)
	if err != nil {
		return 0, nil, err
	}

	if c.activeReader != nil && !c.activeReader.eof() {
		if h.opcode != opContinuation {
			err := errors.New("received new data message without finishing the previous message")
			c.exportedClose(StatusProtocolError, err.Error(), false)
			return 0, nil, err
		}

		if !h.fin || h.payloadLength > 0 {
			return 0, nil, fmt.Errorf("previous message not read to completion")
		}

		c.activeReader = nil

		h, err = c.readTillMsg(ctx)
		if err != nil {
			return 0, nil, err
		}
	} else if h.opcode == opContinuation {
		err := errors.New("received continuation frame not after data or text frame")
		c.exportedClose(StatusProtocolError, err.Error(), false)
		return 0, nil, err
	}

	c.readerMsgCtx = ctx
	c.readerMsgHeader = h
	c.readerFrameEOF = false
	c.readerMaskKey = h.maskKey
	c.readMsgLeft = c.msgReadLimit.Load()

	r := &messageReader{
		c: c,
	}
	c.activeReader = r
	return MessageType(h.opcode), r, nil
}

// messageReader enables reading a data frame from the WebSocket connection.
type messageReader struct {
	c *Conn
}

func (r *messageReader) eof() bool {
	return r.c.activeReader != r
}

// Read reads as many bytes as possible into p.
func (r *messageReader) Read(p []byte) (int, error) {
	return r.exportedRead(p, true)
}

func (r *messageReader) exportedRead(p []byte, lock bool) (int, error) {
	n, err := r.read(p, lock)
	if err != nil {
		// Have to return io.EOF directly for now, we cannot wrap as errors.Is
		// isn't used widely yet.
		if errors.Is(err, io.EOF) {
			return n, io.EOF
		}
		return n, fmt.Errorf("failed to read: %w", err)
	}
	return n, nil
}

func (r *messageReader) readUnlocked(p []byte) (int, error) {
	return r.exportedRead(p, false)
}

func (r *messageReader) read(p []byte, lock bool) (int, error) {
	if lock {
		// If we cannot acquire the read lock, then
		// there is either a concurrent read or the close handshake
		// is proceeding.
		select {
		case r.c.readLock <- struct{}{}:
			defer r.c.releaseLock(r.c.readLock)
		default:
			if r.c.closing.Load() == 1 {
				<-r.c.closed
				return 0, r.c.closeErr
			}
			return 0, errors.New("concurrent read detected")
		}
	}

	if r.eof() {
		return 0, errors.New("cannot use EOFed reader")
	}

	if r.c.readMsgLeft <= 0 {
		err := fmt.Errorf("read limited at %v bytes", r.c.msgReadLimit)
		r.c.exportedClose(StatusMessageTooBig, err.Error(), false)
		return 0, err
	}

	if int64(len(p)) > r.c.readMsgLeft {
		p = p[:r.c.readMsgLeft]
	}

	if r.c.readerFrameEOF {
		h, err := r.c.readTillMsg(r.c.readerMsgCtx)
		if err != nil {
			return 0, err
		}

		if h.opcode != opContinuation {
			err := errors.New("received new data message without finishing the previous message")
			r.c.exportedClose(StatusProtocolError, err.Error(), false)
			return 0, err
		}

		r.c.readerMsgHeader = h
		r.c.readerFrameEOF = false
		r.c.readerMaskKey = h.maskKey
	}

	h := r.c.readerMsgHeader
	if int64(len(p)) > h.payloadLength {
		p = p[:h.payloadLength]
	}

	n, err := r.c.readFramePayload(r.c.readerMsgCtx, p)

	h.payloadLength -= int64(n)
	r.c.readMsgLeft -= int64(n)
	if h.masked {
		r.c.readerMaskKey = mask(r.c.readerMaskKey, p)
	}
	r.c.readerMsgHeader = h

	if err != nil {
		return n, err
	}

	if h.payloadLength == 0 {
		r.c.readerFrameEOF = true

		if h.fin {
			r.c.activeReader = nil
			return n, io.EOF
		}
	}

	return n, nil
}

func (c *Conn) readFramePayload(ctx context.Context, p []byte) (_ int, err error) {
	wrap := func(err error) error {
		return fmt.Errorf("failed to read frame payload: %w", err)
	}
	defer func() {
		if err != nil {
			err = wrap(err)
		}
	}()

	err = c.acquireLock(ctx, c.readFrameLock)
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
		case <-ctx.Done():
			err = ctx.Err()
		default:
		}
		c.releaseLock(c.readFrameLock)
		c.close(wrap(err))
		return n, err
	}

	select {
	case <-c.closed:
		return n, c.closeErr
	case c.setReadTimeout <- context.Background():
	}

	return n, err
}

// Read is a convenience method to read a single message from the connection.
//
// See the Reader method if you want to be able to reuse buffers or want to stream a message.
// The docs on Reader apply to this method as well.
func (c *Conn) Read(ctx context.Context) (MessageType, []byte, error) {
	typ, r, err := c.Reader(ctx)
	if err != nil {
		return 0, nil, err
	}

	b, err := ioutil.ReadAll(r)
	return typ, b, err
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
		return nil, fmt.Errorf("failed to get writer: %w", err)
	}
	return wc, nil
}

func (c *Conn) writer(ctx context.Context, typ MessageType) (io.WriteCloser, error) {
	err := c.acquireLock(ctx, c.writeMsgLock)
	if err != nil {
		return nil, err
	}
	c.writeMsgCtx = ctx
	c.writeMsgOpcode = opcode(typ)
	w := &messageWriter{
		c: c,
	}
	c.activeWriter.Store(w)
	return w, nil
}

// Write is a convenience method to write a message to the connection.
//
// See the Writer method if you want to stream a message.
func (c *Conn) Write(ctx context.Context, typ MessageType, p []byte) error {
	_, err := c.write(ctx, typ, p)
	if err != nil {
		return fmt.Errorf("failed to write msg: %w", err)
	}
	return nil
}

func (c *Conn) write(ctx context.Context, typ MessageType, p []byte) (int, error) {
	err := c.acquireLock(ctx, c.writeMsgLock)
	if err != nil {
		return 0, err
	}
	defer c.releaseLock(c.writeMsgLock)

	n, err := c.writeFrame(ctx, true, opcode(typ), p)
	return n, err
}

// messageWriter enables writing to a WebSocket connection.
type messageWriter struct {
	c *Conn
}

func (w *messageWriter) closed() bool {
	return w != w.c.activeWriter.Load()
}

// Write writes the given bytes to the WebSocket connection.
func (w *messageWriter) Write(p []byte) (int, error) {
	n, err := w.write(p)
	if err != nil {
		return n, fmt.Errorf("failed to write: %w", err)
	}
	return n, nil
}

func (w *messageWriter) write(p []byte) (int, error) {
	if w.closed() {
		return 0, fmt.Errorf("cannot use closed writer")
	}
	n, err := w.c.writeFrame(w.c.writeMsgCtx, false, w.c.writeMsgOpcode, p)
	if err != nil {
		return n, fmt.Errorf("failed to write data frame: %w", err)
	}
	w.c.writeMsgOpcode = opContinuation
	return n, nil
}

// Close flushes the frame to the connection.
// This must be called for every messageWriter.
func (w *messageWriter) Close() error {
	err := w.close()
	if err != nil {
		return fmt.Errorf("failed to close writer: %w", err)
	}
	return nil
}

func (w *messageWriter) close() error {
	if w.closed() {
		return fmt.Errorf("cannot use closed writer")
	}
	w.c.activeWriter.Store((*messageWriter)(nil))

	_, err := w.c.writeFrame(w.c.writeMsgCtx, true, w.c.writeMsgOpcode, nil)
	if err != nil {
		return fmt.Errorf("failed to write fin frame: %w", err)
	}

	w.c.releaseLock(w.c.writeMsgLock)
	return nil
}

func (c *Conn) writeControl(ctx context.Context, opcode opcode, p []byte) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	_, err := c.writeFrame(ctx, true, opcode, p)
	if err != nil {
		return fmt.Errorf("failed to write control frame %v: %w", opcode, err)
	}
	return nil
}

// writeFrame handles all writes to the connection.
func (c *Conn) writeFrame(ctx context.Context, fin bool, opcode opcode, p []byte) (int, error) {
	err := c.acquireLock(ctx, c.writeFrameLock)
	if err != nil {
		return 0, err
	}
	defer c.releaseLock(c.writeFrameLock)

	select {
	case <-c.closed:
		return 0, c.closeErr
	case c.setWriteTimeout <- ctx:
	}

	c.writeHeader.fin = fin
	c.writeHeader.opcode = opcode
	c.writeHeader.masked = c.client
	c.writeHeader.payloadLength = int64(len(p))

	if c.client {
		err = binary.Read(rand.Reader, binary.LittleEndian, &c.writeHeader.maskKey)
		if err != nil {
			return 0, fmt.Errorf("failed to generate masking key: %w", err)
		}
	}

	n, err := c.realWriteFrame(ctx, *c.writeHeader, p)
	if err != nil {
		return n, err
	}

	// We already finished writing, no need to potentially brick the connection if
	// the context expires.
	select {
	case <-c.closed:
		return n, c.closeErr
	case c.setWriteTimeout <- context.Background():
	}

	return n, nil
}

func (c *Conn) realWriteFrame(ctx context.Context, h header, p []byte) (n int, err error) {
	defer func() {
		if err != nil {
			select {
			case <-c.closed:
				err = c.closeErr
			case <-ctx.Done():
				err = ctx.Err()
			default:
			}

			err = fmt.Errorf("failed to write %v frame: %w", h.opcode, err)
			// We need to release the lock first before closing the connection to ensure
			// the lock can be acquired inside close to ensure no one can access c.bw.
			c.releaseLock(c.writeFrameLock)
			c.close(err)
		}
	}()

	headerBytes := writeHeader(c.writeHeaderBuf, h)
	_, err = c.bw.Write(headerBytes)
	if err != nil {
		return 0, err
	}

	if c.client {
		maskKey := h.maskKey
		for len(p) > 0 {
			if c.bw.Available() == 0 {
				err = c.bw.Flush()
				if err != nil {
					return n, err
				}
			}

			// Start of next write in the buffer.
			i := c.bw.Buffered()

			p2 := p
			if len(p) > c.bw.Available() {
				p2 = p[:c.bw.Available()]
			}

			n2, err := c.bw.Write(p2)
			if err != nil {
				return n, err
			}

			maskKey = mask(maskKey, c.writeBuf[i:i+n2])

			p = p[n2:]
			n += n2
		}
	} else {
		n, err = c.bw.Write(p)
		if err != nil {
			return n, err
		}
	}

	if h.fin {
		err = c.bw.Flush()
		if err != nil {
			return n, err
		}
	}

	return n, nil
}

// Close closes the WebSocket connection with the given status code and reason.
//
// It will write a WebSocket close frame with a timeout of 5s and then wait 5s for
// the peer to send a close frame.
// Thus, it implements the full WebSocket close handshake.
// All data messages received from the peer during the close handshake
// will be discarded.
//
// The connection can only be closed once. Additional calls to Close
// are no-ops.
//
// The maximum length of reason must be 125 bytes otherwise an internal
// error will be sent to the peer. For this reason, you should avoid
// sending a dynamic reason.
//
// Close will unblock all goroutines interacting with the connection once
// complete.
func (c *Conn) Close(code StatusCode, reason string) error {
	err := c.exportedClose(code, reason, true)
	var ec errClosing
	if errors.As(err, &ec) {
		<-c.closed
		// We wait until the connection closes.
		// We use writeClose and not exportedClose to avoid a second failed to marshal close frame error.
		err = c.writeClose(nil, ec.ce, true)
	}
	if err != nil {
		return fmt.Errorf("failed to close websocket connection: %w", err)
	}
	return nil
}

func (c *Conn) exportedClose(code StatusCode, reason string, handshake bool) error {
	ce := CloseError{
		Code:   code,
		Reason: reason,
	}

	// This function also will not wait for a close frame from the peer like the RFC
	// wants because that makes no sense and I don't think anyone actually follows that.
	// Definitely worth seeing what popular browsers do later.
	p, err := ce.bytes()
	if err != nil {
		c.logf("websocket: failed to marshal close frame: %+v", err)
		ce = CloseError{
			Code: StatusInternalError,
		}
		p, _ = ce.bytes()
	}

	return c.writeClose(p, fmt.Errorf("sent close: %w", ce), handshake)
}

type errClosing struct {
	ce error
}

func (e errClosing) Error() string {
	return "already closing connection"
}

func (c *Conn) writeClose(p []byte, ce error, handshake bool) error {
	if c.isClosed() {
		return fmt.Errorf("tried to close with %q but connection already closed: %w", ce, c.closeErr)
	}

	if !c.closing.CAS(0, 1) {
		// Normally, we would want to wait until the connection is closed,
		// at least for when a user calls into Close, so we handle that case in
		// the exported Close function.
		//
		// But for internal library usage, we always want to return early, e.g.
		// if we are performing a close handshake and the peer sends their close frame,
		// we do not want to block here waiting for c.closed to close because it won't,
		// at least not until we return since the gorouine that will close it is this one.
		return errClosing{
			ce: ce,
		}
	}

	// No matter what happens next, close error should be set.
	c.setCloseErr(ce)
	defer c.close(nil)

	err := c.writeControl(context.Background(), opClose, p)
	if err != nil {
		return err
	}

	if handshake {
		err = c.waitClose()
		if CloseStatus(err) == -1 {
			// waitClose exited not due to receiving a close frame.
			return fmt.Errorf("failed to wait for peer close frame: %w", err)
		}
	}

	return nil
}

func (c *Conn) waitClose() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	err := c.acquireLock(ctx, c.readLock)
	if err != nil {
		return err
	}
	defer c.releaseLock(c.readLock)

	if c.closeReceived != nil {
		// goroutine reading just received the close.
		return c.closeReceived
	}

	b := bpool.Get()
	buf := b.Bytes()
	buf = buf[:cap(buf)]
	defer bpool.Put(b)

	for {
		if c.activeReader == nil || c.readerFrameEOF {
			_, _, err := c.reader(ctx, false)
			if err != nil {
				return fmt.Errorf("failed to get reader: %w", err)
			}
		}

		r := readerFunc(c.activeReader.readUnlocked)
		_, err = io.CopyBuffer(ioutil.Discard, r, buf)
		if err != nil {
			return err
		}
	}
}

// Ping sends a ping to the peer and waits for a pong.
// Use this to measure latency or ensure the peer is responsive.
// Ping must be called concurrently with Reader as it does
// not read from the connection but instead waits for a Reader call
// to read the pong.
//
// TCP Keepalives should suffice for most use cases.
func (c *Conn) Ping(ctx context.Context) error {
	p := c.pingCounter.Increment(1)

	err := c.ping(ctx, strconv.FormatInt(p, 10))
	if err != nil {
		return fmt.Errorf("failed to ping: %w", err)
	}
	return nil
}

func (c *Conn) ping(ctx context.Context, p string) error {
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
	case <-c.closed:
		return c.closeErr
	case <-ctx.Done():
		err := fmt.Errorf("failed to wait for pong: %w", ctx.Err())
		c.close(err)
		return err
	case <-pong:
		return nil
	}
}

type readerFunc func(p []byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) {
	return f(p)
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}

// extractBufioWriterBuf grabs the []byte backing a *bufio.Writer
// and stores it in c.writeBuf.
func (c *Conn) extractBufioWriterBuf(w io.Writer) {
	c.bw.Reset(writerFunc(func(p2 []byte) (int, error) {
		c.writeBuf = p2[:cap(p2)]
		return len(p2), nil
	}))

	c.bw.WriteByte(0)
	c.bw.Flush()

	c.bw.Reset(w)
}
