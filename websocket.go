package websocket

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"runtime"
	"sync"
	"time"

	"golang.org/x/xerrors"
)

type controlFrame struct {
	header header
	data   []byte
}

// Conn represents a WebSocket connection.
// Pings will always be automatically responded to with pongs, you do not
// have to do anything special.
type Conn struct {
	subprotocol string
	br          *bufio.Reader
	// TODO Cannot use bufio writer because for compression we need to know how much is buffered and compress it if large.
	bw     *bufio.Writer
	closer io.Closer
	client bool

	closeOnce sync.Once
	closeErr  error
	closed    chan struct{}

	// Writers should send on write to begin sending
	// a message and then follow that up with some data
	// on writeBytes.
	write      chan opcode
	writeBytes chan []byte

	// Readers should receive on read to begin reading a message.
	// Then send a byte slice to readBytes to read into it.
	// The n of bytes read will be sent on readDone once the read into a slice is complete.
	// readDone will receive 0 when EOF is reached.
	read      chan opcode
	readBytes chan []byte
	readDone  chan int
}

func (c *Conn) getCloseErr() error {
	if c.closeErr == nil {
		return xerrors.New("websocket: use of closed connection")
	}
	return c.closeErr
}

func (c *Conn) close(err error) {
	if err != nil {
		err = xerrors.Errorf("websocket: connection broken: %v", err)
	}

	c.closeOnce.Do(func() {
		runtime.SetFinalizer(c, nil)

		c.closeErr = err

		cerr := c.closer.Close()
		if c.closeErr == nil {
			c.closeErr = cerr
		}

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
	c.write = make(chan opcode)
	c.read = make(chan opcode)
	c.readDone = make(chan int)
	c.readBytes = make(chan []byte)

	runtime.SetFinalizer(c, func(c *Conn) {
		c.Close(StatusInternalError, "websocket: connection ended up being garbage collected")
	})

	go c.writeLoop()
	go c.readLoop()
}

func (c *Conn) writeLoop() {
messageLoop:
	for {
		c.writeBytes = make(chan []byte)
		var opcode opcode
		select {
		case <-c.closed:
			return
		case opcode = <-c.write:
		}

		var firstSent bool
		for {
			select {
			case <-c.closed:
				return
			case b, ok := <-c.writeBytes:
				if !firstSent || !opcode.controlOp() {
					h := header{
						fin:           opcode.controlOp() || !ok,
						opcode:        opcode,
						payloadLength: int64(len(b)),
						masked:        c.client,
					}

					if firstSent {
						h.opcode = opContinuation
					}
					firstSent = true

					b2 := marshalHeader(h)
					_, err := c.bw.Write(b2)
					if err != nil {
						c.close(xerrors.Errorf("failed to write to connection: %v", err))
						return
					}

					_, err = c.bw.Write(b)
					if err != nil {
						c.close(xerrors.Errorf("failed to write to connection: %v", err))
						return
					}
				}

				if ok {
					select {
					case <-c.closed:
						return
					case c.writeBytes <- nil:
					}
				} else {
					err := c.bw.Flush()
					if err != nil {
						c.close(xerrors.Errorf("failed to write to connection: %v", err))
						return
					}
					if opcode == opClose {
						c.close(nil)
						return
					}
					continue messageLoop
				}
			}
		}
	}
}

func (c *Conn) handleControl(h header) {
	if h.payloadLength > maxControlFramePayload {
		c.Close(StatusProtocolError, "control frame too large")
		return
	}

	b := make([]byte, h.payloadLength)
	_, err := io.ReadFull(c.br, b)
	if err != nil {
		c.close(xerrors.Errorf("failed to read control frame payload: %v", err))
		return
	}

	if h.masked {
		mask(h.maskKey, 0, b)
	}

	switch h.opcode {
	case opPing:
		c.writePong(b)
	case opPong:
	case opClose:
		code, reason, err := parseClosePayload(b)
		if err != nil {
			c.close(xerrors.Errorf("read invalid close payload: %v", err))
			return
		}
		c.Close(code, reason)
	default:
		panic(fmt.Sprintf("websocket: unexpected control opcode: %#v", h))
	}
}

func (c *Conn) readLoop() {
	var indata bool
	for {
		h, err := readHeader(c.br)
		if err != nil {
			c.close(xerrors.Errorf("failed to read header: %v", err))
			return
		}

		if h.rsv1 || h.rsv2 || h.rsv3 {
			c.Close(StatusProtocolError, fmt.Sprintf("read header with rsv bits set: %v:%v:%v", h.rsv1, h.rsv2, h.rsv3))
			return
		}

		// TODO this is fucked, as if they are reading a frame as they are writing, then we can't send ping/close so we'll just get stuck for 5s.
		switch h.opcode {
		case opClose, opPing, opPong:
			c.handleControl(h)
			continue
		}

		switch h.opcode {
		case opBinary, opText:
		default:
			c.close(xerrors.Errorf("unexpected opcode in header: %#v", h))
			return
		}

		if !indata {
			select {
			case <-c.closed:
				return
			case c.read <- h.opcode:
			}
		} else {
			indata = true
		}

		var maskPos int
		left := h.payloadLength
		for left > 0 {
			select {
			case <-c.closed:
				return
			case b := <-c.readBytes:
				if int64(len(b)) > left {
					b = b[:left]
				}

				_, err = io.ReadFull(c.br, b)
				if err != nil {
					c.close(xerrors.Errorf("failed to read from connection: %v", err))
					return
				}
				left -= int64(len(b))

				if h.masked {
					maskPos = mask(h.maskKey, maskPos, b)
				}

				select {
				case <-c.closed:
					return
				case c.readDone <- len(b):
				}
			}
		}

		if h.fin {
			indata = false
			select {
			case <-c.closed:
				return
			case c.readDone <- 0:
			}
		}
	}
}

func (c *Conn) writePong(p []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	w := c.messageWriter(opPong)
	w.SetContext(ctx)
	_, err := w.Write(p)
	if err != nil {
		return err
	}
	err = w.Close()
	return err
}

// MessageWriter returns a writer bounded by the context that will write
// a WebSocket data frame of type dataType to the connection.
// Ensure you close the MessageWriter once you have written to entire message.
// Concurrent calls to MessageWriter are ok.
func (c *Conn) MessageWriter(dataType DataType) *MessageWriter {
	return c.messageWriter(opcode(dataType))
}

func (c *Conn) messageWriter(opcode opcode) *MessageWriter {
	return &MessageWriter{
		c:      c,
		ctx:    context.Background(),
		opcode: opcode,
	}
}

// ReadMessage will wait until there is a WebSocket data frame to read from the connection.
// It returns the type of the data, a reader to read it and also an error.
// Please use SetContext on the reader to bound the read operation.
// Your application must keep reading messages for the Conn to automatically respond to ping
// and close frames.
func (c *Conn) ReadMessage(ctx context.Context) (DataType, *MessageReader, error) {
	select {
	case <-c.closed:
		return 0, nil, xerrors.Errorf("failed to read message: %v", c.getCloseErr())
	case opcode := <-c.read:
		return DataType(opcode), &MessageReader{
			ctx: context.Background(),
			c:   c,
		}, nil
	case <-ctx.Done():
		return 0, nil, xerrors.Errorf("failed to read message: %v", ctx.Err())
	}
}

// Close closes the WebSocket connection with the given status code and reason.
// It will write a WebSocket close frame with a timeout of 5 seconds.
// TODO close error should become c.closeErr to indicate we closed.
func (c *Conn) Close(code StatusCode, reason string) error {
	// This function also will not wait for a close frame from the peer like the RFC
	// wants because that makes no sense and I don't think anyone actually follows that.
	// Definitely worth seeing what popular browsers do later.
	p, err := closePayload(code, reason)
	if err != nil {
		p, _ = closePayload(StatusInternalError, fmt.Sprintf("websocket: application tried to send code %v but code or reason was invalid", code))
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	select {
	case <-c.closed:
		return c.getCloseErr()
	case c.write <- opClose:
	case <-ctx.Done():
		c.close(xerrors.New("force closed: close frame write timed out"))
		return c.getCloseErr()
	}

	select {
	case <-c.closed:
		return c.getCloseErr()
	case c.writeBytes <- p:
		select {
		case <-c.closed:
			return c.getCloseErr()
		case <-c.writeBytes:
			close(c.writeBytes)
		case <-ctx.Done():
			return ctx.Err()
		}
	case <-ctx.Done():
		c.close(xerrors.New("force closed: close frame write timed out"))
		return c.getCloseErr()
	}

	select {
	case <-c.closed:
		if err != nil {
			return err
		}
		return c.closeErr
	case <-ctx.Done():
		c.close(xerrors.New("force closed: close frame write timed out"))
		return c.getCloseErr()
	}
}

// MessageWriter enables writing to a WebSocket connection.
// Ensure you close the MessageWriter once you have written to entire message.
type MessageWriter struct {
	opcode       opcode
	ctx          context.Context
	c            *Conn
	acquiredLock bool
	sentFirst    bool

	done chan struct{}
}

// Write writes the given bytes to the WebSocket connection.
// The frame will automatically be fragmented as appropriate
// with the buffers obtained from http.Hijacker.
// Please ensure you call Close once you have written the full message.
func (w *MessageWriter) Write(p []byte) (int, error) {
	if !w.acquiredLock {
		select {
		case <-w.c.closed:
			return 0, w.c.getCloseErr()
		case w.c.write <- w.opcode:
			w.acquiredLock = true
		case <-w.ctx.Done():
			return 0, w.ctx.Err()
		}
	}

	select {
	case <-w.c.closed:
		return 0, w.c.getCloseErr()
	case w.c.writeBytes <- p:
		select {
		case <-w.c.closed:
			return 0, w.c.getCloseErr()
		case <-w.c.writeBytes:
			return len(p), nil
		case <-w.ctx.Done():
			return 0, w.ctx.Err()
		}
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	}
}

// SetContext bounds the writer to the context.
func (w *MessageWriter) SetContext(ctx context.Context) {
	w.ctx = ctx
}

// Close flushes the frame to the connection.
// This must be called for every MessageWriter.
func (w *MessageWriter) Close() error {
	if !w.acquiredLock {
		select {
		case <-w.c.closed:
			return w.c.getCloseErr()
		case w.c.write <- w.opcode:
			w.acquiredLock = true
		case <-w.ctx.Done():
			return w.ctx.Err()
		}
	}
	close(w.c.writeBytes)
	return nil
}

// MessageReader enables reading a data frame from the WebSocket connection.
type MessageReader struct {
	n     int
	limit int
	c     *Conn
	ctx   context.Context
}

// SetContext bounds the read operation to the ctx.
// By default, the context is the one passed to conn.ReadMessage.
// You still almost always want a separate context for reading the message though.
func (r *MessageReader) SetContext(ctx context.Context) {
	r.ctx = ctx
}

// Limit limits the number of bytes read by the reader.
func (r *MessageReader) Limit(bytes int) {
	r.limit = bytes
}

// Read reads as many bytes as possible into p.
func (r *MessageReader) Read(p []byte) (n int, err error) {
	select {
	case <-r.c.closed:
		return 0, r.c.getCloseErr()
	case <-r.c.readDone:
		return 0, io.EOF
	case r.c.readBytes <- p:
		select {
		case <-r.c.closed:
			return 0, r.c.getCloseErr()
		case n := <-r.c.readDone:
			r.n += n
			// TODO make this better later and inside readLoop to prevent the read from actually occuring if over limit.
			if r.limit > 0 && r.n > r.limit {
				return 0, xerrors.New("message too big")
			}
			return n, nil
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		}
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	}
}
