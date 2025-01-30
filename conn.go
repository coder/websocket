package websocket

import (
	"context"
	"io"
)

type Conn struct {
	Stream
}

type Stream interface {
	Close(code StatusCode, reason string) (err error)
	CloseNow() (err error)
	CloseRead(ctx context.Context) context.Context
	Ping(ctx context.Context) error
	Read(ctx context.Context) (MessageType, []byte, error)
	Reader(ctx context.Context) (MessageType, io.Reader, error)
	SetReadLimit(n int64)
	Subprotocol() string
	Write(ctx context.Context, typ MessageType, p []byte) error
	Writer(ctx context.Context, typ MessageType) (io.WriteCloser, error)
	conn() any
	newMu() muLocker
}

type muLocker interface {
	forceLock()
	tryLock() bool
	unlock()
}
