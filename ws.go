package ws

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http/httpguts"
	"golang.org/x/time/rate"
)

// No exported constructor because very few callers will want to pass in a custom net conn.
// If a fast upgrade mechanism is necessary without overhead of net/http, I would rather provide
// a wrapper here.
type Conn struct {
	client bool

	// I cannot see anyone customizing these and they are not changeable in the http2 or
	// http libraries either so I think a solid default is all that matters.
	readRateLimiter rate.Limiter
	br              *bufio.Reader

	writeMu  chan struct{}
	writes   chan write
	lastRead atomic.Value
	bw       *bufio.Writer

	errored chan struct{}
	errMu   sync.Mutex
	err     error

	conn net.Conn
}

type write struct {
	h       header
	payload []byte
}

func newConn(br *bufio.Reader, bw *bufio.Writer, netConn net.Conn, client bool) *Conn {
	c := &Conn{
		client: client,

		br: br,

		writeMu: make(chan struct{}, 1),
		writes:  make(chan write),
		bw:      bw,

		errored: make(chan struct{}),

		conn: netConn,
	}
	c.lastRead.Store(time.Now())
	go func() {
		err := c.manage()
		if err != nil {
			c.Close()
			c.err = err
			close(c.errored)
		}
	}()
	return c
}

func (c *Conn) manage() error {
	const pingInterval = time.Minute
	pingsTimer := time.NewTicker(pingInterval)
	defer pingsTimer.Stop()

	for {
		select {
		case <-pingsTimer.C:
			lastRead := c.lastRead.Load().(time.Time)
			if time.Since(lastRead) < pingInterval {
				continue
			}
			if time.Since(lastRead) >= pingInterval*2 {
				return fmt.Errorf("connection died; no read activity in %v; last read at %v", pingInterval*2, lastRead)
			}
			h := header{
				fin:    true,
				opCode: opPing,
				masked: c.client,
			}
			b, err := h.bytes()
			if err != nil {
				return fmt.Errorf("failed to create bytes for ping header %#v: %v", h, err)
			}
			_, err = c.bw.Write(b)
			if err != nil {
				return fmt.Errorf("failed to write ping header bytes: %v", err)
			}
		case w := <-c.writes:
			b, err := w.h.bytes()
			if err != nil {
				return fmt.Errorf("failed to create bytes for header %#v: %v", w.h, err)
			}

			_, err = c.bw.Write(b)
			if err != nil {
				return fmt.Errorf("failed to write header bytes: %v", err)
			}

			_, err = c.bw.Write(w.payload)
			if err != nil {
				return fmt.Errorf("failed to write payload: %v", err)
			}

			if w.h.fin {
				err = c.bw.Flush()
				if err != nil {
					return fmt.Errorf("failed to flush writer: %v", err)
				}
			}
		case <-c.errored:
			return nil
		}
	}
}

func (c *Conn) setErr(err error) {
	c.errMu.Lock()
	if c.err == nil {
		c.err = err
		close(c.errored)
	}
	c.errMu.Unlock()
}

type MessageWriter struct {
	c *Conn

	ctx       context.Context
	cancelCtx context.CancelFunc

	opCode Opcode
	locked bool
}

// No error because in stdlib they use net.Conn.SetDeadline without checking error often.
// Only error case for TCP is if the conn is closed.
// If the deadline is zero, that disables the deadline from the default
// of 10 seconds.
func (mw *MessageWriter) SetContext(ctx context.Context) {
	mw.ctx = ctx
	mw.cancelCtx = func() {}
}

func (mw *MessageWriter) Close() (err error) {
	defer func() {
		if err != nil {
			mw.c.setErr(err)
		}
	}()

	mw.cancelCtx()

	select {
	case <-mw.c.errored:
		return fmt.Errorf("connection has already failed: %v", mw.c.err)
	default:
	}

	if !mw.locked {
		return errors.New("message writer must have write lock before call Close; please call Write first")
	}

	defer mw.c.releaseWriteLock()

	h := header{
		fin:    true,
		opCode: opContinuation,
		masked: mw.c.client,
	}
	err = mw.c.write(mw.ctx, h, nil)
	if err != nil {
		return fmt.Errorf("failed to create header bytes: %v", err)
	}
	return nil
}

func (c *Conn) acquireWriteLock(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("message writer context expired: %v", ctx.Err())
	case <-c.errored:
		return fmt.Errorf("connection failed while waiting for write mutex: %v", c.err)
	case c.writeMu <- struct{}{}:
		return nil
	}
}

func (c *Conn) releaseWriteLock() {
	<-c.writeMu
}

// TODO need more context on all my errors
func (mw *MessageWriter) Write(p []byte) (n int, err error) {
	defer func() {
		if err != nil {
			mw.c.setErr(err)
		}
	}()
	opCode := opContinuation
	if !mw.locked {
		if mw.ctx == nil {
			mw.ctx, mw.cancelCtx = context.WithTimeout(context.Background(), time.Second*10)
		}
		err := mw.c.acquireWriteLock(mw.ctx)
		if err != nil {
			return 0, err
		}
		opCode = mw.opCode
		mw.locked = true
	}

	h := header{
		opCode:        opCode,
		payloadLength: int64(len(p)),
		masked:        mw.c.client,
	}

	err = mw.c.write(mw.ctx, h, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *Conn) write(ctx context.Context, h header, payload []byte) error {
	w := write{
		h:       h,
		payload: payload,
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("context expired: %v", ctx.Err())
	case c.writes <- w:
		return nil
	case <-c.errored:
		return fmt.Errorf("connection failed while writing: %v", c.err)
	}
}

func (c *Conn) MessageWriter(op Opcode) *MessageWriter {
	if op != OpBinary && op != OpText {
		panicf("cannot write non binary and non text data frame: %v", op)
	}
	return c.writeDataMessage(Opcode(op))
}

func (c *Conn) Close(code StatusCode, reason []byte) error {
	p, err := closePayload(code, reason)
	if err != nil {
		return fmt.Errorf("failed to construct close payload: %v", err)
	}
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()
	h := header{
		fin:           true,
		opCode:        opClose,
		masked:        c.client,
		payloadLength: int64(len(p)),
	}
	return c.write(ctx, h, p)
}

func panicf(f string, v ...interface{}) {
	msg := fmt.Sprintf(f, v...)
	panic(msg)
}

func (c *Conn) writeDataMessage(op Opcode) *MessageWriter {
	mw := &MessageWriter{
		c:      c,
		opCode: op,
	}
	return mw
}

type MessageReader struct {
	c *Conn
	h header

	limit     int
	ctx       context.Context
	cancelCtx context.CancelFunc
}

// No error because in stdlib they use net.Conn.SetDeadline without checking error often.
// Only error case for TCP is if the conn is closed.
func (mr *MessageReader) SetContext(ctx context.Context) {
	mr.ctx = ctx
	mr.cancelCtx = func() {}
}

// Do not use io.LimitReader because it does not error if the limit was hit.
func (mr *MessageReader) Limit(n int) {
	mr.limit = n
}

func (mr *MessageReader) Read(p []byte) (n int, err error) {
	if mr.ctx == nil {
		mr.ctx, mr.cancelCtx = context.WithTimeout(context.Background(), time.Second*30)
	}
	panic("TODO")
}

func (c *Conn) updateLastRead() {
	c.lastRead.Store(time.Now())
}

func (c *Conn) ReadMessage(ctx context.Context) (typ Opcode, payload *MessageReader, err error) {
	// TODO use ctx
	for {
		h, err := readHeader(c.br)
		if err != nil {
			return 0, nil, err
		}
		c.updateLastRead()

		switch h.opCode {
		case opPong:
			continue
		case opPing:
			panic("TODO")
		case opClose:
			panic("TODO")
		}

		mr := &MessageReader{
			c: c,
			h: h,

			limit: 32768,
		}
		return h.opCode, mr, nil
	}
}

func httpHeaderContains(h http.Header, headerName, token string) error {
	if !httpguts.HeaderValuesContainsToken(h[headerName], token) {
		return fmt.Errorf(`%v header must contain %v: %q`, headerName, token, h[headerName])
	}
	return nil
}

func ishttpUpgradeHeader(h http.Header) error {
	err := httpHeaderContains(h, "Connection", "Upgrade")
	if err != nil {
		return err
	}
	err = httpHeaderContains(h, "Upgrade", "websocket")
	return err
}

func ServerHandshake(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	err := ishttpUpgradeHeader(r.Header)
	if err != nil {
		http.Error(w, "missing upgrade header for WebSocket upgrade", http.StatusBadRequest)
		return nil, err
	}
	if r.Method != http.MethodGet {
		return nil, fmt.Errorf("method must be %q: %q", http.MethodGet, r.Method)
	}
	if !r.ProtoAtLeast(1, 1) {
		return nil, fmt.Errorf("protocol must be at least HTTP/1.1: %q", r.Proto)
	}
	if !httpguts.HeaderValuesContainsToken(r.Header["Sec-WebSocket-Version"], "13") {
		return nil, fmt.Errorf(`Sec-WebSocket-Version must contain "13": %q`, r.Header["Sec-WebSocket-Version"])
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("missing Sec-WebSocket-Key header")
	}

	w.Header().Set("Sec-WebSocket-Accept", secWebSocketAccept(key))

	// Need to set this to prevent net/http from using chunked transfer encoding by default.
	w.Header().Set("Content-Length", "0")

	w.WriteHeader(http.StatusSwitchingProtocols)

	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("http.ResponseWriter does not implement http.Hijacker")
	}

	netConn, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}

	c := newConn(brw.Reader, brw.Writer, netConn, false)
	return c, nil
}

var acceptGUID = []byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11")

func secWebSocketAccept(key string) string {
	h := sha1.New()
	io.WriteString(h, key)
	h.Write(acceptGUID)
	hash := h.Sum(nil)
	return base64.StdEncoding.EncodeToString(hash)
}

// We use this key for all client requests.
// See https://stackoverflow.com/a/37074398/4283659
// Also see https://trac.ietf.org/trac/hybi/wiki/FAQ#WhatSec-WebSocket-KeyandSec-WebSocket-Acceptarefor
// The Sec-WebSocket-Key header is useless.
var secWebSocketKey = base64.StdEncoding.EncodeToString(make([]byte, 16))

// Will be removed/modified in Go 1.12 as we can use http.Client to do the handshake.
func ClientHandshake(req *http.Request, netConn net.Conn) (c *Conn, resp *http.Response, err error) {
	if req.Method != http.MethodGet {
		return nil, nil, fmt.Errorf("handshake request must use method %v, got %v", http.MethodGet, req.Method)
	}

	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", secWebSocketKey)

	br := bufio.NewReader(netConn)
	bw := bufio.NewWriter(netConn)

	err = req.Write(bw)
	if err != nil {
		return nil, nil, err
	}

	err = bw.Flush()
	if err != nil {
		return nil, nil, err
	}

	resp, err = http.ReadResponse(br, req)
	if err != nil {
		return nil, nil, err
	}

	err = verifyServerResponse(resp)
	if err != nil {
		return nil, resp, err
	}

	conn := newConn(br, bw, netConn, true)
	return conn, resp, nil
}

// TODO link rfc everywhere
// Never check Sec-WebSocket-Accept because its useless.
func verifyServerResponse(resp *http.Response) error {
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("expected status code %v but got: %v", http.StatusSwitchingProtocols, resp.StatusCode)
	}
	err := ishttpUpgradeHeader(resp.Header)
	// TODO maybe check key?
	return err
}
