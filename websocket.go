package websocket

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
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
// TODO set finalizer
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
	write      chan opcode
	writeBytes chan []byte

	// Readers should receive on read to begin reading a message.
	// Then send a byte slice to readBytes to read into it.
	// A value on done will be sent once the read into a slice is complete.
	// done will be closed when the message has been fully read.
	read      chan opcode
	readBytes chan []byte
	readDone  chan struct{}
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
	c.readBytes = make(chan []byte)

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
				if !ok {
					if !opcode.controlOp() {
						h := header{
							fin:    true,
							opcode: opContinuation,
							masked: c.client,
						}
						b = marshalHeader(h)
						_, err := c.bw.Write(b)
						if err != nil {
							c.close(xerrors.Errorf("failed to write to connection: %v", err))
							return
						}
					}
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

				h := header{
					fin:           opcode.controlOp(),
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
		}
	}
}

func (c *Conn) readLoop() {
	for {
		h, err := readHeader(c.br)
		if err != nil {
			c.close(xerrors.Errorf("failed to read header: %v", err))
			return
		}

		switch h.opcode {
		case opClose, opPing:
			if h.payloadLength > maxControlFramePayload {
				c.Close(StatusProtocolError, "control frame too large")
				return
			}
			b := make([]byte, h.payloadLength)
			_, err = io.ReadFull(c.br, b)
			if err != nil {
				c.close(xerrors.Errorf("failed to read control frame payload: %v", err))
				return
			}

			if h.opcode == opPing {
				c.writePing(b)
				continue
			}

			code, reason, err := parseClosePayload(b)
			if err != nil {
				c.close(xerrors.Errorf("invalid close payload: %v", err))
				return
			}
			c.Close(code, reason)
			return
		}

		switch h.opcode {
		case opBinary, opText:
		default:
			c.close(xerrors.Errorf("unexpected opcode in header: %#v", h))
			return
		}

		c.readDone = make(chan struct{})
		c.read <- h.opcode
		for {
			var maskPos int
			left := h.payloadLength
			for left > 0 {
				select {
				case <-c.closed:
					return
				case b := <-c.readBytes:
					log.Println("readbytes", left)

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
					case c.readDone <- struct{}{}:
					}
				}
			}

			if h.fin {
				break
			}
			h, err = readHeader(c.br)
			if err != nil {
				c.close(xerrors.Errorf("failed to read header: %v", err))
				return
			}
			// TODO check opcode.
		}
		close(c.readDone)
	}
}

func (c *Conn) writePing(p []byte) {
	panic("TODO")
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
		return 0, nil, c.getCloseErr()
	case opcode := <-c.read:
		return DataType(opcode), &MessageReader{
			ctx: context.Background(),
			c:   c,
		}, nil
	case <-ctx.Done():
		return 0, nil, ctx.Err()
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
	}

	select {
	case <-c.closed:
		return c.getCloseErr()
	case c.writeBytes <- p:
		close(c.writeBytes)
	case <-ctx.Done():
		c.close(xerrors.New("force closed: close frame write timed out"))
	}

	select {
	case <-c.closed:
	case <-ctx.Done():
		c.close(xerrors.New("force closed: close frame write timed out"))
	}
	if err != nil {
		return err
	}
	return c.closeErr
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
		return len(p), nil
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
		return xerrors.New("websocket: MessageWriter closed without writing any bytes")
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
		case <-r.c.readDone:
			r.n += len(p)
			// TODO make this better later and inside readLoop to prevent the read from actually occuring if over limit.
			if r.limit > 0 && n > r.limit {
				return 0, xerrors.New("message too big")
			}
			return len(p), nil
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		}
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	}
}
