//go:build !js

package websocket

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// MessageType represents the type of a WebSocket message.
// See https://tools.ietf.org/html/rfc6455#section-5.6
type MessageType int

// MessageType constants.
const (
	// MessageText is for UTF-8 encoded text messages like JSON.
	MessageText MessageType = iota + 1
	// MessageBinary is for binary messages like protobufs.
	MessageBinary
)

// Conn represents a WebSocket connection.
// All methods may be called concurrently except for Reader and Read.
//
// You must always read from the connection. Otherwise control
// frames will not be handled. See Reader and CloseRead.
//
// Be sure to call Close on the connection when you
// are finished with it to release associated resources.
//
// On any error from any method, the connection is closed
// with an appropriate reason.
//
// This applies to context expirations as well unfortunately.
// See https://github.com/nhooyr/websocket/issues/242#issuecomment-633182220
type Conn struct {
	noCopy noCopy

	subprotocol    string
	rwc            io.ReadWriteCloser
	client         bool
	copts          *compressionOptions
	flateThreshold int
	br             *bufio.Reader
	bw             *bufio.Writer

	readTimeoutStop  atomic.Pointer[func() bool]
	writeTimeoutStop atomic.Pointer[func() bool]

	// writeDeadline is the deadline set by SetWriteDeadline as unix nanos,
	// or 0 for none. writeDeadlineTimer is created once per connection and
	// thereafter only Reset and Stopped, neither of which allocates — the
	// same approach netconn.go takes for net.Conn deadlines.
	//
	// The timer is armed by writeFrame rather than by SetWriteDeadline, so
	// that it is armed under writeFrameMu together with the frame it
	// bounds. That keeps it safe when several goroutines write to one
	// connection: setting a deadline cannot disturb a frame already in
	// flight, and frames written under one deadline share it instead of
	// restarting the clock per frame.
	writeDeadline      atomic.Int64
	writeDeadlineTimer *time.Timer

	// Read state.
	readMu         *mu
	readHeaderBuf  [8]byte
	readControlBuf [maxControlPayload]byte
	msgReader      *msgReader

	// Write state.
	msgWriter      *msgWriter
	writeFrameMu   *mu
	writeBuf       []byte
	writeHeaderBuf [8]byte
	writeHeader    header

	// Close handshake state.
	closeStateMu     sync.RWMutex
	closeReceivedErr error
	closeSentErr     error

	// CloseRead state.
	closeReadMu   sync.Mutex
	closeReadCtx  context.Context
	closeReadDone chan struct{}

	closing atomic.Bool
	closeMu sync.Mutex // Protects following.
	closed  chan struct{}

	pingCounter    atomic.Int64
	activePingsMu  sync.Mutex
	activePings    map[string]chan<- struct{}
	onPingReceived func(context.Context, []byte) bool
	onPongReceived func(context.Context, []byte)
}

type connConfig struct {
	subprotocol    string
	rwc            io.ReadWriteCloser
	client         bool
	copts          *compressionOptions
	flateThreshold int
	onPingReceived func(context.Context, []byte) bool
	onPongReceived func(context.Context, []byte)

	br *bufio.Reader
	bw *bufio.Writer
}

func newConn(cfg connConfig) *Conn {
	c := &Conn{
		subprotocol:    cfg.subprotocol,
		rwc:            cfg.rwc,
		client:         cfg.client,
		copts:          cfg.copts,
		flateThreshold: cfg.flateThreshold,

		br: cfg.br,
		bw: cfg.bw,

		closed:         make(chan struct{}),
		activePings:    make(map[string]chan<- struct{}),
		onPingReceived: cfg.onPingReceived,
		onPongReceived: cfg.onPongReceived,
	}

	c.readMu = newMu(c)
	c.writeFrameMu = newMu(c)

	c.msgReader = newMsgReader(c)

	c.msgWriter = newMsgWriter(c)
	if c.client {
		c.writeBuf = extractBufioWriterBuf(c.bw, c.rwc)
	}

	if c.flate() && c.flateThreshold == 0 {
		c.flateThreshold = 128
		if !c.msgWriter.flateContextTakeover() {
			c.flateThreshold = 512
		}
	}

	// One timer for the life of the connection, so that arming a write
	// deadline is a Reset and disarming it a Stop, neither of which
	// allocates. The duration is irrelevant because it is stopped
	// immediately: there is no constructor for a stopped timer, and this is
	// how netconn.go makes one.
	c.writeDeadlineTimer = time.AfterFunc(math.MaxInt64, func() {
		c.close()
	})
	c.writeDeadlineTimer.Stop()

	runtime.SetFinalizer(c, func(c *Conn) {
		c.close()
	})

	return c
}

// Subprotocol returns the negotiated subprotocol.
// An empty string means the default protocol.
func (c *Conn) Subprotocol() string {
	return c.subprotocol
}

func (c *Conn) close() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()

	if c.isClosed() {
		return net.ErrClosed
	}
	runtime.SetFinalizer(c, nil)
	close(c.closed)

	// Have to close after c.closed is closed to ensure any goroutine that wakes up
	// from the connection being closed also sees that c.closed is closed and returns
	// closeErr.
	err := c.rwc.Close()
	// With the close of rwc, these become safe to close.
	c.msgWriter.close()
	c.msgReader.close()
	return err
}

func (c *Conn) setupWriteTimeout(ctx context.Context) bool {
	if ctx.Done() == nil {
		return false
	}

	stop := context.AfterFunc(ctx, func() {
		c.clearWriteTimeout()
		c.close()
	})
	swapTimeoutStop(&c.writeTimeoutStop, &stop)
	return true
}

// SetWriteDeadline sets a deadline for writes on the connection. Writes
// that pass it close the connection, as an expired context passed to Write
// does. A zero t clears the deadline.
//
// It exists so that a deadline can be expressed without allocating.
// Bounding a write with a context costs a context.WithTimeout in the caller
// and a context.AfterFunc inside this package, per frame; a deadline is an
// instant, and an instant is one atomic store against a timer that is
// reused for the life of the connection. Callers that pass a
// non-cancellable context to Write and set the deadline here allocate
// nothing on the write path, which matters for a server fanning one message
// out to many connections, where that per-frame cost is paid per recipient.
//
// The deadline belongs to the connection, not to a call, so where several
// goroutines write to one connection the last to set it wins. Frames
// written under one deadline all wait for that same instant rather than
// restarting the clock, and setting a deadline never disturbs a frame
// already in flight.
func (c *Conn) SetWriteDeadline(t time.Time) error {
	if t.IsZero() {
		c.writeDeadline.Store(0)
		return nil
	}
	c.writeDeadline.Store(t.UnixNano())
	return nil
}

// armWriteDeadline prepares the write deadline for one frame. It reports
// whether the deadline has already passed, and whether the timer was armed
// and so needs stopping once the frame is done. The caller must hold
// writeFrameMu.
//
// It must not close the connection itself, even though an expired deadline
// means the connection is going away: close takes writeFrameMu for a client
// connection, via msgWriter.close, and this runs with that lock held. That
// is why expiry is reported back to the caller and the closing is left to
// the timer's own goroutine, which holds nothing.
//
// It reports bools rather than returning the stop func it would rather
// return, because a closure over c is a heap allocation on every frame,
// which would defeat the point of the mechanism.
func (c *Conn) armWriteDeadline() (expired, armed bool) {
	deadline := c.writeDeadline.Load()
	if deadline == 0 {
		return false, false
	}

	d := time.Until(time.Unix(0, deadline))
	if d <= 0 {
		// Already past: refuse the frame, and let the timer do the closing
		// from where that is safe. Deliberately left running.
		c.writeDeadlineTimer.Reset(1)
		return true, false
	}
	c.writeDeadlineTimer.Reset(d)
	return false, true
}

func (c *Conn) stopWriteDeadline() { c.writeDeadlineTimer.Stop() }

func (c *Conn) clearWriteTimeout() {
	swapTimeoutStop(&c.writeTimeoutStop, nil)
}

func (c *Conn) setupReadTimeout(ctx context.Context) bool {
	if ctx.Done() == nil {
		return false
	}

	stop := context.AfterFunc(ctx, func() {
		c.clearReadTimeout()
		c.close()
	})
	swapTimeoutStop(&c.readTimeoutStop, &stop)
	return true
}

func (c *Conn) clearReadTimeout() {
	swapTimeoutStop(&c.readTimeoutStop, nil)
}

func swapTimeoutStop(p *atomic.Pointer[func() bool], newStop *func() bool) {
	oldStop := p.Swap(newStop)
	if oldStop != nil {
		(*oldStop)()
	}
}

func (c *Conn) flate() bool {
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
	p := c.pingCounter.Add(1)

	err := c.ping(ctx, strconv.FormatInt(p, 10))
	if err != nil {
		return fmt.Errorf("failed to ping: %w", err)
	}
	return nil
}

func (c *Conn) ping(ctx context.Context, p string) error {
	pong := make(chan struct{}, 1)

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
		return net.ErrClosed
	case <-ctx.Done():
		return fmt.Errorf("failed to wait for pong: %w", ctx.Err())
	case <-pong:
		return nil
	}
}

type mu struct {
	c  *Conn
	ch chan struct{}
}

func newMu(c *Conn) *mu {
	return &mu{
		c:  c,
		ch: make(chan struct{}, 1),
	}
}

func (m *mu) forceLock() {
	m.ch <- struct{}{}
}

func (m *mu) tryLock() bool {
	select {
	case m.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (m *mu) lock(ctx context.Context) error {
	select {
	case <-m.c.closed:
		return net.ErrClosed
	case <-ctx.Done():
		return fmt.Errorf("failed to acquire lock: %w", ctx.Err())
	case m.ch <- struct{}{}:
		// To make sure the connection is certainly alive.
		// As it's possible the send on m.ch was selected
		// over the receive on closed.
		select {
		case <-m.c.closed:
			// Make sure to release.
			m.unlock()
			return net.ErrClosed
		default:
		}
		return nil
	}
}

func (m *mu) unlock() {
	select {
	case <-m.ch:
	default:
	}
}

type noCopy struct{}

func (*noCopy) Lock() {}
