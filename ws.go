package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/http/httpguts"
)

type Conn struct {
	client bool

	// I cannot see anyone customizing these and they are not changeable in the http2 or
	// http libraries either so I think a solid default is all that matters.
	br   *bufio.Reader
	bw   *bufio.Writer
	conn net.Conn
}

// No exported constructor because very few callers will want to pass in a custom net conn.
// If a fast upgrade mechanism is necessary without overhead of net/http, I would rather provide
// a wrapper here.
func newConn(r io.Reader, w io.Writer, conn net.Conn) *Conn {
	return &Conn{
		br:   bufio.NewReader(r),
		bw:   bufio.NewWriter(w),
		conn: conn,
	}
}

func (c *Conn) NetConn() net.Conn {
	return c.conn
}

type MessageWriter struct {
	c *Conn
}

// No error because in stdlib they use net.Conn.SetDeadline without checking error often.
// Only error case for TCP is if the conn is closed.
func (mw MessageWriter) SetDeadline(deadline time.Time) {
	panic("TODO")
}

func (mw MessageWriter) Close() error {
	panic("TODO")
}

func (mw MessageWriter) Write(p []byte) (n int, err error) {
	panic("TODO")
}

func (c *Conn) WriteDataMessage(op DataOpcode) MessageWriter {
	if op != OpBinary && op != OpText {
		panicf("cannot write non binary or text message: %v", op)
	}
	return c.writeMessage(opCode(op))
}

func panicf(f string, v ...interface{}) {
	msg := fmt.Sprintf(f, v...)
	panic(msg)
}

func (c *Conn) writeMessage(op opCode) MessageWriter {
	panic("TODO")
};

type MessageReader struct {
	c *Conn
}

// No error because in stdlib they use net.Conn.SetDeadline without checking error often.
// Only error case for TCP is if the conn is closed.
func (mr MessageReader) SetDeadline(deadline time.Time) {
	panic("TODO")
}

// Do not use io.LimitReader because it does not error if the limit was hit.
func (mr MessageReader) SetLimit(n int) error {
	panic("TODO")
}

func (mr MessageReader) Read(p []byte) (n int, err error) {
	panic("TODO")
}

func (c *Conn) ReadDataMessage() (typ DataOpcode, payload MessageReader, err error) {
	panic("TODO")
}

func (c *Conn) Close() error {
	return c.conn.Close()
}

func (c *Conn) WriteCloseMessage(code StatusCode, p []byte, deadline time.Time) (err error) {
	w := c.writeMessage(opClose)

	w.SetDeadline(deadline)

	err = writeClosePayload(w, code, p)
	if err != nil {
		return err
	}

	err = w.Close()
	return err
}

func ServerHandshake(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if !httpguts.HeaderValuesContainsToken(r.Header["Connection"], "Upgrade") {
		return nil, fmt.Errorf(`Connection header must contain "Upgrade": %q`, r.Header["Connection"])
	}
	if !httpguts.HeaderValuesContainsToken(r.Header["Upgrade"], "websocket") {
		return nil, fmt.Errorf(`Upgrade header must contain "websocket": %q`, r.Header["Upgrade"])
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

	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("http.ResponseWriter does not implement http.Hijacker")
	}

	netConn, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}

	c := newConn(brw.Reader, brw.Writer, netConn)
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

func NewClientHandshake(url string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", secWebSocketKey)
	return req, nil
}

// Will be removed/modified in Go 1.12 as we can use http.Client to do the handshake.
func Go11Handshake(rw *bufio.ReadWriter, req *http.Request) (*Conn, *http.Response, error) {
	// Need to pass in writer explicitly as req.WriteDataMessage handles it specially.
	err := req.Write(rw.Writer)
	if err != nil {
		return nil, nil, err
	}

	err = rw.Flush()
	if err != nil {
		return nil, nil, err
	}

	resp, err := http.ReadResponse(rw.Reader, req)
	if err != nil {
		return nil, nil, err
	}

	err = clientUpgrade(resp)
	if err != nil {
		return nil, resp, err
	}

	return conn, resp, nil
}

// TODO link rfc everywhere
func clientUpgrade(resp *http.Response) error {
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("expected status code %v but got: %v", http.StatusSwitchingProtocols, resp.StatusCode)
	}


}
