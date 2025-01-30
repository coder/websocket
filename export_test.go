//go:build !js
// +build !js

package websocket

import (
	"net"

	"github.com/coder/websocket/internal/util"
)

func (c *StdConn) RecordBytesWritten() *int {
	var bytesWritten int
	c.bw.Reset(util.WriterFunc(func(p []byte) (int, error) {
		bytesWritten += len(p)
		return c.rwc.Write(p)
	}))
	return &bytesWritten
}

func (c *StdConn) RecordBytesRead() *int {
	var bytesRead int
	c.br.Reset(util.ReaderFunc(func(p []byte) (int, error) {
		n, err := c.rwc.Read(p)
		bytesRead += n
		return n, err
	}))
	return &bytesRead
}

var ErrClosed = net.ErrClosed

var ExportedDial = dialStd
var SecWebSocketAccept = secWebSocketAccept
var SecWebSocketKey = secWebSocketKey
var VerifyServerResponse = verifyServerResponse

var CompressionModeOpts = CompressionMode.opts
