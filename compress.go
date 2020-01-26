// +build !js

package websocket

import (
	"compress/flate"
	"io"
	"net/http"
	"sync"
)

// CompressionOptions represents the available deflate extension options.
// See https://tools.ietf.org/html/rfc7692
type CompressionOptions struct {
	// Mode controls the compression mode.
	//
	// See docs on CompressionMode.
	Mode CompressionMode

	// Threshold controls the minimum size of a message before compression is applied.
	//
	// Defaults to 512 bytes for CompressionNoContextTakeover and 256 bytes
	// for CompressionContextTakeover.
	Threshold int
}

// CompressionMode represents the modes available to the deflate extension.
// See https://tools.ietf.org/html/rfc7692
//
// A compatibility layer is implemented for the older deflate-frame extension used
// by safari. See https://tools.ietf.org/html/draft-tyoshino-hybi-websocket-perframe-deflate-06
// It will work the same in every way except that we cannot signal to the peer we
// want to use no context takeover on our side, we can only signal that they should.
type CompressionMode int

const (
	// CompressionNoContextTakeover grabs a new flate.Reader and flate.Writer as needed
	// for every message. This applies to both server and client side.
	//
	// This means less efficient compression as the sliding window from previous messages
	// will not be used but the memory overhead will be lower if the connections
	// are long lived and seldom used.
	//
	// The message will only be compressed if greater than 512 bytes.
	CompressionNoContextTakeover CompressionMode = iota

	// CompressionContextTakeover uses a flate.Reader and flate.Writer per connection.
	// This enables reusing the sliding window from previous messages.
	// As most WebSocket protocols are repetitive, this can be very efficient.
	// It carries an overhead of 64 kB for every connection compared to CompressionNoContextTakeover.
	//
	// If the peer negotiates NoContextTakeover on the client or server side, it will be
	// used instead as this is required by the RFC.
	CompressionContextTakeover

	// CompressionDisabled disables the deflate extension.
	//
	// Use this if you are using a predominantly binary protocol with very
	// little duplication in between messages or CPU and memory are more
	// important than bandwidth.
	CompressionDisabled
)

func (m CompressionMode) opts() *compressionOptions {
	if m == CompressionDisabled {
		return nil
	}
	return &compressionOptions{
		clientNoContextTakeover: m == CompressionNoContextTakeover,
		serverNoContextTakeover: m == CompressionNoContextTakeover,
	}
}

type compressionOptions struct {
	clientNoContextTakeover bool
	serverNoContextTakeover bool
}

func (copts *compressionOptions) setHeader(h http.Header) {
	s := "permessage-deflate"
	if copts.clientNoContextTakeover {
		s += "; client_no_context_takeover"
	}
	if copts.serverNoContextTakeover {
		s += "; server_no_context_takeover"
	}
	h.Set("Sec-WebSocket-Extensions", s)
}

// These bytes are required to get flate.Reader to return.
// They are removed when sending to avoid the overhead as
// WebSocket framing tell's when the message has ended but then
// we need to add them back otherwise flate.Reader keeps
// trying to return more bytes.
const deflateMessageTail = "\x00\x00\xff\xff"

func (c *Conn) writeNoContextTakeOver() bool {
	return c.client && c.copts.clientNoContextTakeover || !c.client && c.copts.serverNoContextTakeover
}

func (c *Conn) readNoContextTakeOver() bool {
	return !c.client && c.copts.clientNoContextTakeover || c.client && c.copts.serverNoContextTakeover
}

type trimLastFourBytesWriter struct {
	w    io.Writer
	tail []byte
}

func (tw *trimLastFourBytesWriter) reset() {
	tw.tail = tw.tail[:0]
}

func (tw *trimLastFourBytesWriter) Write(p []byte) (int, error) {
	extra := len(tw.tail) + len(p) - 4

	if extra <= 0 {
		tw.tail = append(tw.tail, p...)
		return len(p), nil
	}

	// Now we need to write as many extra bytes as we can from the previous tail.
	if extra > len(tw.tail) {
		extra = len(tw.tail)
	}
	if extra > 0 {
		_, err := tw.w.Write(tw.tail[:extra])
		if err != nil {
			return 0, err
		}
		tw.tail = tw.tail[extra:]
	}

	// If p is less than or equal to 4 bytes,
	// all of it is is part of the tail.
	if len(p) <= 4 {
		tw.tail = append(tw.tail, p...)
		return len(p), nil
	}

	// Otherwise, only the last 4 bytes are.
	tw.tail = append(tw.tail, p[len(p)-4:]...)

	p = p[:len(p)-4]
	n, err := tw.w.Write(p)
	return n + 4, err
}

var flateReaderPool sync.Pool

func getFlateReader(r io.Reader) io.Reader {
	fr, ok := flateReaderPool.Get().(io.Reader)
	if !ok {
		return flate.NewReader(r)
	}
	fr.(flate.Resetter).Reset(r, nil)
	return fr
}

func putFlateReader(fr io.Reader) {
	flateReaderPool.Put(fr)
}

var flateWriterPool sync.Pool

func getFlateWriter(w io.Writer, dict []byte) *flate.Writer {
	fw, ok := flateWriterPool.Get().(*flate.Writer)
	if !ok {
		fw, _ = flate.NewWriterDict(w, flate.BestSpeed, dict)
		return fw
	}
	fw.Reset(w)
	return fw
}

func putFlateWriter(w *flate.Writer) {
	flateWriterPool.Put(w)
}

type slidingWindowReader struct {
	window []byte

	r io.Reader
}

func (r slidingWindowReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	p = p[:n]

	r.append(p)

	return n, err
}

func (r slidingWindowReader) append(p []byte) {
	if len(r.window) <= cap(r.window) {
		r.window = append(r.window, p...)
	}

	if len(p) > cap(r.window) {
		p = p[len(p)-cap(r.window):]
	}

	// p now contains at max the last window bytes
	// so we need to be able to append all of it to r.window.
	// Shift as many bytes from r.window as needed.

	// Maximum window size minus current window minus extra gives
	// us the number of bytes that need to be shifted.
	off := len(r.window) + len(p) - cap(r.window)

	r.window = append(r.window[:0], r.window[off:]...)
	copy(r.window, r.window[off:])
	copy(r.window[len(r.window)-len(p):], p)
	return
}
