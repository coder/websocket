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
	"strings"
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
	// TODO reconsider this re bufio read/writers given the open issues on adding support in net/http for this.
	readRateLimiter rate.Limiter
	br              *bufio.Reader

	writeMu  chan struct{}
	writes   chan write
	lastRead atomic.Value
	// Cannot use *bufio.Writer because we want to modify the buffered bytes for masking.
	// This also enables us to optimize the last frame send by modifying the FIN bit instead
	// if sending a empty FIN frame.
	writeBuf []byte

	errored chan struct{}
	errMu   sync.Mutex
	err     error

	netConn io.Closer
}

type write struct {
	h       header
	payload []byte
}

func newClientConn(netConn io.Closer, br *bufio.Reader, bw *bufio.Writer) *Conn {
	return newConn(netConn, br, bw, true)
}

func newServerConn(netConn io.Closer, br *bufio.Reader, bw *bufio.Writer) *Conn {
	return newConn(netConn, br, bw, false)
}

func newConn(netConn io.Closer, br *bufio.Reader, bw *bufio.Writer, client bool) *Conn {
	c := &Conn{
		client: client,

		br: br,

		writeMu:  make(chan struct{}, 1),
		writes:   make(chan write),
		writeBuf: bufioWriterBuffer(bw),

		errored: make(chan struct{}),

		netConn: netConn,
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

func (c *Conn) Subprotocol() string {
	panic("TODO")
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

func (mw *MessageWriter) Compress() {
	panic("TODO")
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
	// TODO can't do this, need to block until write is finished
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
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
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
		mr.ctx, mr.cancelCtx = context.WithTimeout(context.Background(), time.Second*10)
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

// isHttpUpgradeHeader checks if the header is a WebSocket upgrade header.
// The returned error is safe to show publically.
func ishttpUpgradeHeader(h http.Header) error {
	err := httpHeaderContains(h, "Connection", "Upgrade")
	if err != nil {
		return err
	}
	err = httpHeaderContains(h, "Upgrade", "websocket")
	return err
}

func serverHandshake(opts upgradeOptions, w http.ResponseWriter, r *http.Request) (err error, status int) {
	err, status = Origins(w, r, opts.Origins...)
	if err != nil {
		return err, status
	}

	err = ishttpUpgradeHeader(r.Header)
	if err != nil {
		return err, http.StatusBadRequest
	}
	if r.Method != http.MethodGet {
		err = fmt.Errorf("method must be %v; was %v", http.MethodGet, r.Method)
		return err, http.StatusMethodNotAllowed
	}
	if !r.ProtoAtLeast(1, 1) {
		err = fmt.Errorf("protocol must be at least HTTP/1.1: %v", r.Proto)
		return err, http.StatusBadRequest
	}
	if !httpguts.HeaderValuesContainsToken(r.Header["Sec-WebSocket-Version"], "13") {
		err = fmt.Errorf(`Sec-WebSocket-Version must contain "13": %q`, r.Header["Sec-WebSocket-Version"])
		return err, http.StatusBadRequest
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		err = errors.New("missing Sec-WebSocket-Key header")
		return err, http.StatusBadRequest
	}

	w.Header().Set("Sec-WebSocket-Accept", secWebSocketAccept(key))

	// Need to set this to prevent net/http from using chunked transfer encoding by default.
	w.Header().Set("Content-Length", "0")

	w.WriteHeader(http.StatusSwitchingProtocols)

	return nil, 0
}

func splitHeader(h string) []string {
	h = strings.TrimSpace(h)
	if h == "" {
		return nil
	}
	values := strings.Split(h, ",")
	for i := range values {
		values[i] = strings.TrimSpace(values[i])
	}
	return values
}

// Extensions returns a client's extensions.
func Extensions(r *http.Request) []string {
	h := r.Header.Get("Sec-WebSocket-Extensions")
	return splitHeader(h)
}

// Subprotocols returns the subprotocols requested by the client.
// Select one by setting it as the value of the Sec-WebSocket-Protocol response header.
func Subprotocols(subprotocols []string) UpgradeOption {
	h := r.Header.Get("Sec-WebSocket-Protocol")
	return splitHeader(h)
}

func AllowNullProtocol() UpgradeOption {
	return nil
}

type upgradeOptions struct {
	authorizeOrigin func(origin string) bool
	selectSubProtocol func(r *http.Request) ([]string, error)
}

type UpgradeOption func(o *upgradeOptions)

func Upgrade(w http.ResponseWriter, r *http.Request, opts ...UpgradeOption) (*Conn, error) {
	uopts := &upgradeOptions{
		authorizeOrigin: func(origin string) bool {
			return false
		},
		selectSubProtocol: func(r *http.Request) ([]string, error) {
			return nil, nil
		},
	}

	err, status := serverHandshake(w, r, )
	if err != nil {
		msg := fmt.Sprintf("WebSocket handshake error: %v.", err)
		http.Error(w, msg, status)
		return nil, err
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		msg := fmt.Sprintf("WebSocket upgrade not supported on protocol %v.", r.Proto)
		http.Error(w, msg, http.StatusBadRequest)
		return nil, errors.New("http.ResponseWriter does not implement http.Hijacker")
	}

	netConn, brw, err := hj.Hijack()
	if err != nil {
		http.Error(w, "Failed to upgrade to WebSocket.", http.StatusInternalServerError)
		return nil, fmt.Errorf("failed to hijack connection: %v", err)
	}

	conn := newServerConn(netConn, brw.Reader, brw.Writer)
	return conn, nil
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

type dialOptions struct {

}

type DialOption func(o *dialOptions)

func Dial(url string, opts ...DialOption) (*Conn, *http.Response, error) {
	if req.Method != http.MethodGet {
		return nil, nil, fmt.Errorf("handshake request must use method %v, got %v", http.MethodGet, req.Method)
	}

	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", secWebSocketKey)

	br := bufio.NewReader(netConn)
	bw := bufio.NewWriter(netConn)

	err := req.Write(bw)
	if err != nil {
		return nil, nil, err
	}

	err = bw.Flush()
	if err != nil {
		return nil, nil, err
	}

	resp, err := http.ReadResponse(br, req)
	if err != nil {
		return nil, nil, err
	}

	err = verifyServerResponse(resp)
	if err != nil {
		return nil, resp, err
	}

	conn := newClientConn(netConn, br, bw)
	return conn, resp, nil
}

func verifyServerResponse(resp *http.Response) error {
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("expected status code %v but got: %v", http.StatusSwitchingProtocols, resp.StatusCode)
	}
	err := ishttpUpgradeHeader(resp.Header)
	// TODO maybe check key?
	return err
}

type writerFunc func(p []byte) (n int, err error)

func (r writerFunc) Write(p []byte) (n int, err error) {
	return r(p)
}

func bufioWriterBuffer(bw *bufio.Writer) []byte {
	// This code assumes that bufio.Writer.buf[:1] is passed to the
	// bufio.Writer's underlying writer.
	var writeBuf []byte
	r := writerFunc(func(p []byte) (n int, err error) {
		writeBuf = p[:cap(p)]
		return len(p), nil
	})
	bw.Reset(r)
	bw.WriteByte(0)
	bw.Flush()

	return writeBuf
}
