package websocket // import "nhooyr.io/websocket"

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"runtime"
	"sync"
	"syscall/js"

	"nhooyr.io/websocket/internal/bpool"
	"nhooyr.io/websocket/internal/wsjs"
)

// Conn provides a wrapper around the browser WebSocket API.
type Conn struct {
	ws wsjs.WebSocket

	// read limit for a message in bytes.
	msgReadLimit *atomicInt64

	readClosed   *atomicInt64
	closeOnce    sync.Once
	closed       chan struct{}
	closeErrOnce sync.Once
	closeErr     error

	releaseOnClose   func()
	releaseOnMessage func()

	readSignal chan struct{}
	readBufMu  sync.Mutex
	readBuf    []wsjs.MessageEvent
}

func (c *Conn) close(err error) {
	c.closeOnce.Do(func() {
		runtime.SetFinalizer(c, nil)

		c.setCloseErr(err)
		close(c.closed)
	})
}

func (c *Conn) init() {
	c.closed = make(chan struct{})
	c.readSignal = make(chan struct{}, 1)

	c.msgReadLimit = &atomicInt64{}
	c.msgReadLimit.Store(32768)

	c.readClosed = &atomicInt64{}

	c.releaseOnClose = c.ws.OnClose(func(e wsjs.CloseEvent) {
		cerr := CloseError{
			Code:   StatusCode(e.Code),
			Reason: e.Reason,
		}

		c.close(fmt.Errorf("received close frame: %w", cerr))

		c.releaseOnClose()
		c.releaseOnMessage()
	})

	c.releaseOnMessage = c.ws.OnMessage(func(e wsjs.MessageEvent) {
		c.readBufMu.Lock()
		defer c.readBufMu.Unlock()

		c.readBuf = append(c.readBuf, e)

		// Lets the read goroutine know there is definitely something in readBuf.
		select {
		case c.readSignal <- struct{}{}:
		default:
		}
	})

	runtime.SetFinalizer(c, func(c *Conn) {
		c.setCloseErr(errors.New("connection garbage collected"))
		c.closeWithInternal()
	})
}

func (c *Conn) closeWithInternal() {
	c.Close(StatusInternalError, "something went wrong")
}

// Read attempts to read a message from the connection.
// The maximum time spent waiting is bounded by the context.
func (c *Conn) Read(ctx context.Context) (MessageType, []byte, error) {
	if c.readClosed.Load() == 1 {
		return 0, nil, fmt.Errorf("websocket connection read closed")
	}

	typ, p, err := c.read(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to read: %w", err)
	}
	if int64(len(p)) > c.msgReadLimit.Load() {
		c.Close(StatusMessageTooBig, fmt.Sprintf("read limited at %v bytes", c.msgReadLimit))
		return 0, nil, c.closeErr
	}
	return typ, p, nil
}

func (c *Conn) read(ctx context.Context) (MessageType, []byte, error) {
	select {
	case <-ctx.Done():
		c.Close(StatusPolicyViolation, "read timed out")
		return 0, nil, ctx.Err()
	case <-c.readSignal:
	case <-c.closed:
		return 0, nil, c.closeErr
	}

	c.readBufMu.Lock()
	defer c.readBufMu.Unlock()

	me := c.readBuf[0]
	// We copy the messages forward and decrease the size
	// of the slice to avoid reallocating.
	copy(c.readBuf, c.readBuf[1:])
	c.readBuf = c.readBuf[:len(c.readBuf)-1]

	if len(c.readBuf) > 0 {
		// Next time we read, we'll grab the message.
		select {
		case c.readSignal <- struct{}{}:
		default:
		}
	}

	switch p := me.Data.(type) {
	case string:
		return MessageText, []byte(p), nil
	case []byte:
		return MessageBinary, p, nil
	default:
		panic("websocket: unexpected data type from wsjs OnMessage: " + reflect.TypeOf(me.Data).String())
	}
}

// Write writes a message of the given type to the connection.
// Always non blocking.
func (c *Conn) Write(ctx context.Context, typ MessageType, p []byte) error {
	err := c.write(ctx, typ, p)
	if err != nil {
		// Have to ensure the WebSocket is closed after a write error
		// to match the Go API. It can only error if the message type
		// is unexpected or the passed bytes contain invalid UTF-8 for
		// MessageText.
		err := fmt.Errorf("failed to write: %w", err)
		c.setCloseErr(err)
		c.closeWithInternal()
		return err
	}
	return nil
}

func (c *Conn) write(ctx context.Context, typ MessageType, p []byte) error {
	if c.isClosed() {
		return c.closeErr
	}
	switch typ {
	case MessageBinary:
		return c.ws.SendBytes(p)
	case MessageText:
		return c.ws.SendText(string(p))
	default:
		return fmt.Errorf("unexpected message type: %v", typ)
	}
}

func (c *Conn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

// Close closes the websocket with the given code and reason.
func (c *Conn) Close(code StatusCode, reason string) error {
	if c.isClosed() {
		return fmt.Errorf("already closed: %w", c.closeErr)
	}

	err := fmt.Errorf("sent close frame: %v", CloseError{
		Code:   code,
		Reason: reason,
	})

	err2 := c.ws.Close(int(code), reason)
	if err2 != nil {
		err = err2
	}
	c.close(err)

	if !errors.Is(c.closeErr, err) {
		return fmt.Errorf("failed to close websocket: %w", err)
	}

	return nil
}

// Subprotocol returns the negotiated subprotocol.
// An empty string means the default protocol.
func (c *Conn) Subprotocol() string {
	return c.ws.Protocol
}

// DialOptions represents the options available to pass to Dial.
type DialOptions struct {
	// Subprotocols lists the subprotocols to negotiate with the server.
	Subprotocols []string
}

// Dial creates a new WebSocket connection to the given url with the given options.
// The passed context bounds the maximum time spent waiting for the connection to open.
// The returned *http.Response is always nil or the zero value. It's only in the signature
// to match the core API.
func Dial(ctx context.Context, url string, opts *DialOptions) (*Conn, *http.Response, error) {
	c, resp, err := dial(ctx, url, opts)
	if err != nil {
		return nil, resp, fmt.Errorf("failed to websocket dial: %w", err)
	}
	return c, resp, nil
}

func dial(ctx context.Context, url string, opts *DialOptions) (*Conn, *http.Response, error) {
	if opts == nil {
		opts = &DialOptions{}
	}

	ws, err := wsjs.New(url, opts.Subprotocols)
	if err != nil {
		return nil, nil, err
	}

	c := &Conn{
		ws: ws,
	}
	c.init()

	opench := make(chan struct{})
	releaseOpen := ws.OnOpen(func(e js.Value) {
		close(opench)
	})
	defer releaseOpen()

	select {
	case <-ctx.Done():
		c.Close(StatusPolicyViolation, "dial timed out")
		return nil, nil, ctx.Err()
	case <-opench:
	case <-c.closed:
		return c, nil, c.closeErr
	}

	// Have to return a non nil response as the normal API does that.
	return c, &http.Response{}, nil
}

// Reader attempts to read a message from the connection.
// The maximum time spent waiting is bounded by the context.
func (c *Conn) Reader(ctx context.Context) (MessageType, io.Reader, error) {
	typ, p, err := c.Read(ctx)
	if err != nil {
		return 0, nil, err
	}
	return typ, bytes.NewReader(p), nil
}

// Only implemented for use by *Conn.CloseRead in netconn.go
func (c *Conn) reader(ctx context.Context) {
	c.read(ctx)
}

// Writer returns a writer to write a WebSocket data message to the connection.
// It buffers the entire message in memory and then sends it when the writer
// is closed.
func (c *Conn) Writer(ctx context.Context, typ MessageType) (io.WriteCloser, error) {
	return writer{
		c:   c,
		ctx: ctx,
		typ: typ,
		b:   bpool.Get(),
	}, nil
}

type writer struct {
	closed bool

	c   *Conn
	ctx context.Context
	typ MessageType

	b *bytes.Buffer
}

func (w writer) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errors.New("cannot write to closed writer")
	}
	n, err := w.b.Write(p)
	if err != nil {
		return n, fmt.Errorf("failed to write message: %w", err)
	}
	return n, nil
}

func (w writer) Close() error {
	if w.closed {
		return errors.New("cannot close closed writer")
	}
	w.closed = true
	defer bpool.Put(w.b)

	err := w.c.Write(w.ctx, w.typ, w.b.Bytes())
	if err != nil {
		return fmt.Errorf("failed to close writer: %w", err)
	}
	return nil
}
