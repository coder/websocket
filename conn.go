// +build !js

package websocket

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
)

// MessageType represents the type of a WebSocket message.
// See https://tools.ietf.org/html/rfc6455#section-5.6
type MessageType int

// MessageType constants.
const (
	// MessageText is for UTF-8 encoded text messages like JSON.
	MessageText MessageType = iota + 1
	// MessageBinary is for binary messages like Protobufs.
	MessageBinary
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
	rwc         io.ReadWriteCloser
	client      bool
	copts       *compressionOptions

	cr connReader
	cw connWriter

	closed chan struct{}

	closeMu           sync.Mutex
	closeErr          error
	closeHandshakeErr error

	pingCounter   int32
	activePingsMu sync.Mutex
	activePings   map[string]chan<- struct{}
}

type connConfig struct {
	subprotocol string
	rwc         io.ReadWriteCloser
	client      bool
	copts       *compressionOptions

	bw *bufio.Writer
	br *bufio.Reader
}

func newConn(cfg connConfig) *Conn {
	c := &Conn{}
	c.subprotocol = cfg.subprotocol
	c.rwc = cfg.rwc
	c.client = cfg.client
	c.copts = cfg.copts

	c.cr.init(c, cfg.br)
	c.cw.init(c, cfg.bw)

	c.closed = make(chan struct{})
	c.activePings = make(map[string]chan<- struct{})

	runtime.SetFinalizer(c, func(c *Conn) {
		c.close(errors.New("connection garbage collected"))
	})

	go c.timeoutLoop()

	return c
}

// Subprotocol returns the negotiated subprotocol.
// An empty string means the default protocol.
func (c *Conn) Subprotocol() string {
	return c.subprotocol
}

func (c *Conn) close(err error) {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()

	if c.isClosed() {
		return
	}
	close(c.closed)
	runtime.SetFinalizer(c, nil)
	c.setCloseErrNoLock(err)

	// Have to close after c.closed is closed to ensure any goroutine that wakes up
	// from the connection being closed also sees that c.closed is closed and returns
	// closeErr.
	c.rwc.Close()

	go func() {
		c.cr.close()
		c.cw.close()
	}()
}

func (c *Conn) timeoutLoop() {
	readCtx := context.Background()
	writeCtx := context.Background()

	for {
		select {
		case <-c.closed:
			return

		case writeCtx = <-c.cw.timeout:
		case readCtx = <-c.cr.timeout:

		case <-readCtx.Done():
			c.setCloseErr(fmt.Errorf("read timed out: %w", readCtx.Err()))
			c.cw.error(StatusPolicyViolation, errors.New("timed out"))
			return
		case <-writeCtx.Done():
			c.close(fmt.Errorf("write timed out: %w", writeCtx.Err()))
			return
		}
	}
}

func (c *Conn) deflateNegotiated() bool {
	return c.copts != nil
}

// Ping sends a ping to the peer and waits for a pong.
// Use this to measure latency or ensure the peer is responsive.
// Ping must be called concurrently with Reader as it does
// not read from the connection but instead waits for a Reader call
// to read the pong.
//
// TCP Keepalives should suffice for most use cases.
func (c *Conn) Ping(ctx context.Context) error {
	p := atomic.AddInt32(&c.pingCounter, 1)

	err := c.ping(ctx, strconv.Itoa(int(p)))
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

	err := c.cw.control(ctx, opPing, []byte(p))
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

type mu struct {
	once sync.Once
	ch   chan struct{}
}

func (m *mu) init() {
	m.once.Do(func() {
		m.ch = make(chan struct{}, 1)
	})
}

func (m *mu) Lock(ctx context.Context) error {
	m.init()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case m.ch <- struct{}{}:
		return nil
	}
}

func (m *mu) TryLock() bool {
	m.init()
	select {
	case m.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (m *mu) Unlock() {
	<-m.ch
}
