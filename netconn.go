package websocket

import (
	"context"
	"io"
	"math"
	"net"
	"time"

	"golang.org/x/xerrors"
)

// NetConn converts a *websocket.Conn into a net.Conn.
//
// It's for tunneling arbitrary protocols over WebSockets.
// Few users of the library will need this but it's tricky to implement
// correctly and so provided in the library.
// See https://github.com/nhooyr/websocket/issues/100.
//
// Every Write to the net.Conn will correspond to a binary message
// write on *webscoket.Conn.
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
// A received StatusNormalClosure close frame will be translated to EOF when reading.
func NetConn(c *Conn) net.Conn {
	nc := &netConn{
		c: c,
	}

	var cancel context.CancelFunc
	nc.writeContext, cancel = context.WithCancel(context.Background())
	nc.writeTimer = time.AfterFunc(math.MaxInt64, cancel)
	if !nc.writeTimer.Stop() {
		<-nc.writeTimer.C
	}

	nc.readContext, cancel = context.WithCancel(context.Background())
	nc.readTimer = time.AfterFunc(math.MaxInt64, cancel)
	if !nc.readTimer.Stop() {
		<-nc.readTimer.C
	}

	return nc
}

type netConn struct {
	c *Conn

	writeTimer   *time.Timer
	writeContext context.Context

	readTimer   *time.Timer
	readContext context.Context
	eofed       bool

	reader io.Reader
}

var _ net.Conn = &netConn{}

func (c *netConn) Close() error {
	return c.c.Close(StatusNormalClosure, "")
}

func (c *netConn) Write(p []byte) (int, error) {
	err := c.c.Write(c.writeContext, MessageBinary, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *netConn) Read(p []byte) (int, error) {
	if c.eofed {
		return 0, io.EOF
	}

	if c.reader == nil {
		typ, r, err := c.c.Reader(c.readContext)
		if err != nil {
			var ce CloseError
			if xerrors.As(err, &ce) && (ce.Code == StatusNormalClosure) {
				c.eofed = true
				return 0, io.EOF
			}
			return 0, err
		}
		if typ != MessageBinary {
			c.c.Close(StatusUnsupportedData, "can only accept binary messages")
			return 0, xerrors.Errorf("unexpected frame type read for net conn adapter (expected %v): %v", MessageBinary, typ)
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
		if !c.writeTimer.Stop() {
			<-c.writeTimer.C
		}
	} else {
		c.writeTimer.Reset(t.Sub(time.Now()))
	}
	return nil
}

func (c *netConn) SetReadDeadline(t time.Time) error {
	if t.IsZero() {
		if !c.readTimer.Stop() {
			<-c.readTimer.C
		}
	} else {
		c.readTimer.Reset(t.Sub(time.Now()))
	}
	return nil
}
