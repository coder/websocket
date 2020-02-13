// +build !js

package websocket

import (
	"compress/flate"
	"io"
	"net/http"
	"sync"
)

func (m CompressionMode) opts() *compressionOptions {
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

func getFlateReader(r io.Reader, dict []byte) io.Reader {
	fr, ok := flateReaderPool.Get().(io.Reader)
	if !ok {
		return flate.NewReaderDict(r, dict)
	}
	fr.(flate.Resetter).Reset(r, dict)
	return fr
}

func putFlateReader(fr io.Reader) {
	flateReaderPool.Put(fr)
}

var flateWriterPool sync.Pool

func getFlateWriter(w io.Writer) *flate.Writer {
	fw, ok := flateWriterPool.Get().(*flate.Writer)
	if !ok {
		fw, _ = flate.NewWriter(w, flate.BestSpeed)
		return fw
	}
	fw.Reset(w)
	return fw
}

func putFlateWriter(w *flate.Writer) {
	flateWriterPool.Put(w)
}

type slidingWindow struct {
	buf []byte
}

var swPool = map[int]*sync.Pool{}

func newSlidingWindow(n int) *slidingWindow {
	p, ok := swPool[n]
	if !ok {
		p = &sync.Pool{}
		swPool[n] = p
	}
	sw, ok := p.Get().(*slidingWindow)
	if ok {
		return sw
	}
	return &slidingWindow{
		buf: make([]byte, 0, n),
	}
}

func returnSlidingWindow(sw *slidingWindow) {
	sw.buf = sw.buf[:0]
	swPool[cap(sw.buf)].Put(sw)
}

func (w *slidingWindow) write(p []byte) {
	if len(p) >= cap(w.buf) {
		w.buf = w.buf[:cap(w.buf)]
		p = p[len(p)-cap(w.buf):]
		copy(w.buf, p)
		return
	}

	left := cap(w.buf) - len(w.buf)
	if left < len(p) {
		// We need to shift spaceNeeded bytes from the end to make room for p at the end.
		spaceNeeded := len(p) - left
		copy(w.buf, w.buf[spaceNeeded:])
		w.buf = w.buf[:len(w.buf)-spaceNeeded]
	}

	w.buf = append(w.buf, p...)
}
