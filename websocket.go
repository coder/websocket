package websocket

import (
	"bufio"
	"context"
	cryptorand "crypto/rand"
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
// All methods may be called concurrently except for Reader, Read
// and SetReadLimit.
//
// You must always read from the connection. Otherwise control
// frames will not be handled. See the docs on Reader and CloseRead.
//
// Please be sure to call Close on the connection when you
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

	closeOnce    sync.Once
	closeErrOnce sync.Once
	closeErr     error
	closed       chan struct{}

	// writeMsgLock is acquired to write a data message.
	writeMsgLock chan struct{}
	// writeFrameLock is acquired to write a single frame.
	// Effectively meaning whoever holds it gets to write to bw.
	writeFrameLock chan struct{}
	writeHeaderBuf []byte
	writeHeader    *header
	// read limit for a message in bytes.
	msgReadLimit int64

	// messageWriter state.
	writeMsgOpcode opcode
	writeMsgCtx    context.Context
	readMsgLeft    int64

	// Used to ensure the previous reader is read till EOF before allowing
	// a new one.
	previousReader *messageReader
	// readFrameLock is acquired to read from bw.
	readFrameLock     chan struct{}
	readClosed        int64
	readHeaderBuf     []byte
	controlPayloadBuf []byte

	// messageReader state
	readMsgCtx    context.Context
	readMsgHeader header
	readFrameEOF  bool
	readMaskPos   int

	setReadTimeout  chan context.Context
	setWriteTimeout chan context.Context

	activePingsMu sync.Mutex
	activePings   map[string]chan<- struct{}
}

func (c *Conn) init() {
	c.closed = make(chan struct{})

	c.msgReadLimit = 32768

	c.writeMsgLock = make(chan struct{}, 1)
	c.writeFrameLock = make(chan struct{}, 1)

	c.readFrameLock = make(chan struct{}, 1)

	c.setReadTimeout = make(chan context.Context)
	c.setWriteTimeout = make(chan context.Context)

	c.activePings = make(map[string]chan<- struct{})

	c.writeHeaderBuf = makeWriteHeaderBuf()
	c.writeHeader = &header{}
	c.readHeaderBuf = makeReadHeaderBuf()
	c.controlPayloadBuf = make([]byte, maxControlFramePayload)

	runtime.SetFinalizer(c, func(c *Conn) {
		c.close(xerrors.New("connection garbage collected"))
	})

	go c.timeoutLoop()
}

// Subprotocol returns the negotiated subprotocol.
// An empty string means the default protocol.
func (c *Conn) Subprotocol() string {
	return c.subprotocol
}

func (c *Conn) setCloseErr(err error) {
	c.closeErrOnce.Do(func() {
		c.closeErr = xerrors.Errorf("websocket closed: %w", err)
	})
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

		// See comment in dial.go
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
			c.close(xerrors.Errorf("data read timed out: %w", readCtx.Err()))
		case <-writeCtx.Done():
			c.close(xerrors.Errorf("data write timed out: %w", writeCtx.Err()))
		}
	}
}

func (c *Conn) acquireLock(ctx context.Context, lock chan struct{}) error {
	select {
	case <-ctx.Done():
		var err error
		switch lock {
		case c.writeFrameLock, c.writeMsgLock:
			err = xerrors.Errorf("could not acquire write lock: %v", ctx.Err())
		case c.readFrameLock:
			err = xerrors.Errorf("could not acquire read lock: %v", ctx.Err())
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
			err := xerrors.Errorf("received header with rsv bits set: %v:%v:%v", h.rsv1, h.rsv2, h.rsv3)
			c.Close(StatusProtocolError, err.Error())
			return header{}, err
		}

		if h.opcode.controlOp() {
			err = c.handleControl(ctx, h)
			if err != nil {
				return header{}, xerrors.Errorf("failed to handle control frame: %w", err)
			}
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

func (c *Conn) readFrameHeader(ctx context.Context) (header, error) {
	err := c.acquireLock(context.Background(), c.readFrameLock)
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
		err := xerrors.Errorf("failed to read header: %w", err)
		c.releaseLock(c.readFrameLock)
		c.close(err)
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
		err := xerrors.Errorf("control frame too large at %v bytes", h.payloadLength)
		c.Close(StatusProtocolError, err.Error())
		return err
	}

	if !h.fin {
		err := xerrors.Errorf("received fragmented control frame")
		c.Close(StatusProtocolError, err.Error())
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
		fastXOR(h.maskKey, 0, b)
	}

	switch h.opcode {
	case opPing:
		return c.writePong(b)
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
			c.Close(StatusProtocolError, "received invalid close payload")
			return xerrors.Errorf("received invalid close payload: %w", err)
		}
		// This ensures the closeErr of the Conn is always the received CloseError
		// in case the echo close frame write fails.
		// See https://github.com/nhooyr/websocket/issues/109
		c.setCloseErr(xerrors.Errorf("received close frame: %w", ce))
		c.writeClose(b, nil)
		return c.closeErr
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
	if atomic.LoadInt64(&c.readClosed) == 1 {
		return 0, nil, xerrors.Errorf("websocket connection read closed")
	}

	typ, r, err := c.reader(ctx)
	if err != nil {
		return 0, nil, xerrors.Errorf("failed to get reader: %w", err)
	}
	return typ, r, nil
}

func (c *Conn) reader(ctx context.Context) (MessageType, io.Reader, error) {
	if c.previousReader != nil && !c.readFrameEOF {
		// The only way we know for sure the previous reader is not yet complete is
		// if there is an active frame not yet fully read.
		// Otherwise, a user may have read the last byte but not the EOF if the EOF
		// is in the next frame so we check for that below.
		return 0, nil, xerrors.Errorf("previous message not read to completion")
	}

	h, err := c.readTillMsg(ctx)
	if err != nil {
		return 0, nil, err
	}

	if c.previousReader != nil && !c.previousReader.eof {
		if h.opcode != opContinuation {
			err := xerrors.Errorf("received new data message without finishing the previous message")
			c.Close(StatusProtocolError, err.Error())
			return 0, nil, err
		}

		if !h.fin || h.payloadLength > 0 {
			return 0, nil, xerrors.Errorf("previous message not read to completion")
		}

		c.previousReader.eof = true

		h, err = c.readTillMsg(ctx)
		if err != nil {
			return 0, nil, err
		}
	} else if h.opcode == opContinuation {
		err := xerrors.Errorf("received continuation frame not after data or text frame")
		c.Close(StatusProtocolError, err.Error())
		return 0, nil, err
	}

	c.readMsgCtx = ctx
	c.readMsgHeader = h
	c.readFrameEOF = false
	c.readMaskPos = 0
	c.readMsgLeft = c.msgReadLimit

	r := &messageReader{
		c: c,
	}
	c.previousReader = r
	return MessageType(h.opcode), r, nil
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
	atomic.StoreInt64(&c.readClosed, 1)

	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer cancel()
		// We use the unexported reader so that we don't get the read closed error.
		c.reader(ctx)
		c.Close(StatusPolicyViolation, "unexpected data message")
	}()
	return ctx
}

// messageReader enables reading a data frame from the WebSocket connection.
type messageReader struct {
	c   *Conn
	eof bool
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
	if r.eof {
		return 0, xerrors.Errorf("cannot use EOFed reader")
	}

	if r.c.readMsgLeft <= 0 {
		err := xerrors.Errorf("read limited at %v bytes", r.c.msgReadLimit)
		r.c.Close(StatusMessageTooBig, err.Error())
		return 0, err
	}

	if int64(len(p)) > r.c.readMsgLeft {
		p = p[:r.c.readMsgLeft]
	}

	if r.c.readFrameEOF {
		h, err := r.c.readTillMsg(r.c.readMsgCtx)
		if err != nil {
			return 0, err
		}

		if h.opcode != opContinuation {
			err := xerrors.Errorf("received new data message without finishing the previous message")
			r.c.Close(StatusProtocolError, err.Error())
			return 0, err
		}

		r.c.readMsgHeader = h
		r.c.readFrameEOF = false
		r.c.readMaskPos = 0
	}

	h := r.c.readMsgHeader
	if int64(len(p)) > h.payloadLength {
		p = p[:h.payloadLength]
	}

	n, err := r.c.readFramePayload(r.c.readMsgCtx, p)

	h.payloadLength -= int64(n)
	r.c.readMsgLeft -= int64(n)
	if h.masked {
		r.c.readMaskPos = fastXOR(h.maskKey, r.c.readMaskPos, p)
	}
	r.c.readMsgHeader = h

	if err != nil {
		return n, err
	}

	if h.payloadLength == 0 {
		r.c.readFrameEOF = true

		if h.fin {
			r.eof = true
			return n, io.EOF
		}
	}

	return n, nil
}

func (c *Conn) readFramePayload(ctx context.Context, p []byte) (int, error) {
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
		case <-ctx.Done():
			err = ctx.Err()
		default:
		}
		err = xerrors.Errorf("failed to read frame payload: %w", err)
		c.releaseLock(c.readFrameLock)
		c.close(err)
		return n, err
	}

	select {
	case <-c.closed:
		return n, c.closeErr
	case c.setReadTimeout <- context.Background():
	}

	return n, err
}

// SetReadLimit sets the max number of bytes to read for a single message.
// It applies to the Reader and Read methods.
//
// By default, the connection has a message read limit of 32768 bytes.
//
// When the limit is hit, the connection will be closed with StatusMessageTooBig.
func (c *Conn) SetReadLimit(n int64) {
	c.msgReadLimit = n
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
		return nil, xerrors.Errorf("failed to get writer: %w", err)
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
	return &messageWriter{
		c: c,
	}, nil
}

// Write is a convenience method to write a message to the connection.
//
// See the Writer method if you want to stream a message.
func (c *Conn) Write(ctx context.Context, typ MessageType, p []byte) error {
	_, err := c.write(ctx, typ, p)
	if err != nil {
		return xerrors.Errorf("failed to write msg: %w", err)
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
	n, err := w.c.writeFrame(w.c.writeMsgCtx, false, w.c.writeMsgOpcode, p)
	if err != nil {
		return n, xerrors.Errorf("failed to write data frame: %w", err)
	}
	w.c.writeMsgOpcode = opContinuation
	return n, nil
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

	_, err := w.c.writeFrame(w.c.writeMsgCtx, true, w.c.writeMsgOpcode, nil)
	if err != nil {
		return xerrors.Errorf("failed to write fin frame: %w", err)
	}

	w.c.releaseLock(w.c.writeMsgLock)
	return nil
}

func (c *Conn) writeControl(ctx context.Context, opcode opcode, p []byte) error {
	_, err := c.writeFrame(ctx, true, opcode, p)
	if err != nil {
		return xerrors.Errorf("failed to write control frame: %w", err)
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
		_, err := io.ReadFull(cryptorand.Reader, c.writeHeader.maskKey[:])
		if err != nil {
			return 0, xerrors.Errorf("failed to generate masking key: %w", err)
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

			err = xerrors.Errorf("failed to write %v frame: %w", h.opcode, err)
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
		var keypos int
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

			keypos = fastXOR(h.maskKey, keypos, c.writeBuf[i:i+n2])

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
// This does not perform a WebSocket close handshake.
// See https://github.com/nhooyr/websocket/issues/103 for details on why.
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

	// CloseErrors sent are made opaque to prevent applications from thinking
	// they received a given status.
	err = c.writeClose(p, xerrors.Errorf("sent close frame: %v", ce))
	if err != nil {
		return err
	}

	if !xerrors.Is(c.closeErr, ce) {
		return c.closeErr
	}

	return nil
}

func (c *Conn) writeClose(p []byte, cerr error) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	// If this fails, the connection had to have died.
	err := c.writeControl(ctx, opClose, p)
	if err != nil {
		return err
	}

	c.close(cerr)

	return nil
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

// Ping sends a ping to the peer and waits for a pong.
// Use this to measure latency or ensure the peer is responsive.
// Ping must be called concurrently with Reader as it does
// not read from the connection but instead waits for a Reader call
// to read the pong.
//
// TCP Keepalives should suffice for most use cases.
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
	case <-c.closed:
		return c.closeErr
	case <-ctx.Done():
		err := xerrors.Errorf("failed to wait for pong: %w", ctx.Err())
		c.close(err)
		return err
	case <-pong:
		return nil
	}
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
