package websocket

import (
	"bufio"
	"context"
	"io"
	"nhooyr.io/websocket/internal/atomicint"
	"nhooyr.io/websocket/internal/wsframe"
	"strings"
)

type reader struct {
	// Acquired before performing any sort of read operation.
	readLock chan struct{}

	c *Conn

	deflateReader io.Reader
	br            *bufio.Reader

	readClosed        *atomicint.Int64
	readHeaderBuf     []byte
	controlPayloadBuf []byte

	msgCtx        context.Context
	msgCompressed bool
	frameHeader   wsframe.Header
	frameMaskKey  uint32
	frameEOF      bool
	deflateTail   strings.Reader
}
