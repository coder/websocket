// This file contains *Conn symbols relevant to both
// Wasm and non Wasm builds.

package websocket

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// NetConn converts a *websocket.Conn into a net.Conn.
//
// It's for tunneling arbitrary protocols over WebSockets.
// Few users of the library will need this but it's tricky to implement
// correctly and so provided in the library.
// See https://github.com/nhooyr/websocket/issues/100.
//
// Every Write to the net.Conn will correspond to a message write of
// the given type on *websocket.Conn.
//
// The passed ctx bounds the lifetime of the net.Conn. If cancelled,
// all reads and writes on the net.Conn will be cancelled.
//
// If a message is read that is not of the correct type, the connection
// will be closed with StatusUnsupportedData and an error will be returned.
//
// Close will close the *websocket.Conn with StatusNormalClosure.
//
// When a deadline is hit, the connection will be closed. This is
// different from most net.Conn implementations where only the
// reading/writing goroutines are interrupted but the connection is kept alive.
//
// The Addr methods will return a mock net.Addr that returns "websocket" for Network
// and "websocket/unknown-addr" for String.
//
// A received StatusNormalClosure or StatusGoingAway close frame will be translated to
// io.EOF when reading.
func NetConn(ctx context.Context, c *Conn, msgType MessageType) net.Conn {
	nc := &netConn{
		c:       c,
		msgType: msgType,
	}

	var cancel context.CancelFunc
	nc.writeContext, cancel = context.WithCancel(ctx)
	nc.writeTimer = time.AfterFunc(math.MaxInt64, cancel)
	if !nc.writeTimer.Stop() {
		<-nc.writeTimer.C
	}

	nc.readContext, cancel = context.WithCancel(ctx)
	nc.readTimer = time.AfterFunc(math.MaxInt64, cancel)
	if !nc.readTimer.Stop() {
		<-nc.readTimer.C
	}

	return nc
}

type netConn struct {
	c       *Conn
	msgType MessageType

	writeTimer   *time.Timer
	writeContext context.Context

	readTimer   *time.Timer
	readContext context.Context

	readMu sync.Mutex
	eofed  bool
	reader io.Reader
}

var _ net.Conn = &netConn{}

func (c *netConn) Close() error {
	return c.c.Close(StatusNormalClosure, "")
}

func (c *netConn) Write(p []byte) (int, error) {
	err := c.c.Write(c.writeContext, c.msgType, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *netConn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	if c.eofed {
		return 0, io.EOF
	}

	if c.reader == nil {
		typ, r, err := c.c.Reader(c.readContext)
		if err != nil {
			var ce CloseError
			if errors.As(err, &ce) && (ce.Code == StatusNormalClosure) || (ce.Code == StatusGoingAway) {
				c.eofed = true
				return 0, io.EOF
			}
			return 0, err
		}
		if typ != c.msgType {
			c.c.Close(StatusUnsupportedData, fmt.Sprintf("unexpected frame type read (expected %v): %v", c.msgType, typ))
			return 0, c.c.closeErr
		}
		c.reader = r
	}

	n, err := c.reader.Read(p)
	if err == io.EOF {
		c.reader = nil
		err = nil
	}
	return n, err
}

type websocketAddr struct {
}

func (a websocketAddr) Network() string {
	return "websocket"
}

func (a websocketAddr) String() string {
	return "websocket/unknown-addr"
}

func (c *netConn) RemoteAddr() net.Addr {
	return websocketAddr{}
}

func (c *netConn) LocalAddr() net.Addr {
	return websocketAddr{}
}

func (c *netConn) SetDeadline(t time.Time) error {
	c.SetWriteDeadline(t)
	c.SetReadDeadline(t)
	return nil
}

func (c *netConn) SetWriteDeadline(t time.Time) error {
	if t.IsZero() {
		c.writeTimer.Stop()
	} else {
		c.writeTimer.Reset(t.Sub(time.Now()))
	}
	return nil
}

func (c *netConn) SetReadDeadline(t time.Time) error {
	if t.IsZero() {
		c.readTimer.Stop()
	} else {
		c.readTimer.Reset(t.Sub(time.Now()))
	}
	return nil
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
	atomic.StoreInt64(&c.isReadClosed, 1)

	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer cancel()
		// We use the unexported reader method so that we don't get the read closed error.
		c.reader(ctx)
		// Either the connection is already closed since there was a read error
		// or the context was cancelled or a message was read and we should close
		// the connection.
		c.Close(StatusPolicyViolation, "unexpected data message")
	}()
	return ctx
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

func (c *Conn) setCloseErr(err error) {
	c.closeErrOnce.Do(func() {
		c.closeErr = fmt.Errorf("websocket closed: %w", err)
	})
}
