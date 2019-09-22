package websocket // import "nhooyr.io/websocket"

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"runtime"
	"sync"
	"syscall/js"

	"golang.org/x/xerrors"

	"nhooyr.io/websocket/internal/wsjs"
)

// Conn provides a wrapper around the browser WebSocket API.
type Conn struct {
	ws wsjs.WebSocket

	closeOnce sync.Once
	closed    chan struct{}
	closeErr  error

	releaseOnClose   func()
	releaseOnMessage func()

	readch chan wsjs.MessageEvent
}

func (c *Conn) close(err error) {
	c.closeOnce.Do(func() {
		runtime.SetFinalizer(c, nil)

		c.closeErr = fmt.Errorf("websocket closed: %w", err)
		close(c.closed)

		c.releaseOnClose()
		c.releaseOnMessage()
	})
}

func (c *Conn) init() {
	c.closed = make(chan struct{})
	c.readch = make(chan wsjs.MessageEvent, 1)

	c.releaseOnClose = c.ws.OnClose(func(e wsjs.CloseEvent) {
		cerr := CloseError{
			Code:   StatusCode(e.Code),
			Reason: e.Reason,
		}

		c.close(fmt.Errorf("received close frame: %w", cerr))
	})

	c.releaseOnMessage = c.ws.OnMessage(func(e wsjs.MessageEvent) {
		c.readch <- e
	})

	runtime.SetFinalizer(c, func(c *Conn) {
		c.ws.Close(int(StatusInternalError), "")
		c.close(errors.New("connection garbage collected"))
	})
}

// Read attempts to read a message from the connection.
// The maximum time spent waiting is bounded by the context.
func (c *Conn) Read(ctx context.Context) (MessageType, []byte, error) {
	typ, p, err := c.read(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to read: %w", err)
	}
	return typ, p, nil
}

func (c *Conn) read(ctx context.Context) (MessageType, []byte, error) {
	var me wsjs.MessageEvent
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case me = <-c.readch:
	case <-c.closed:
		return 0, nil, c.closeErr
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
		return fmt.Errorf("failed to write: %w", err)
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

	if !xerrors.Is(c.closeErr, err) {
		return xerrors.Errorf("failed to close websocket: %w", err)
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
		return nil, nil, ctx.Err()
	case <-opench:
	case <-c.closed:
		return c, nil, c.closeErr
	}

	// Have to return a non nil response as the normal API does that.
	return c, &http.Response{}, nil
}
