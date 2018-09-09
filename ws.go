package ws

import (
	"io"
	"nhooyr.io/ws/internal/wscore"
)

type FrameType = wscore.Opcode

const (
	continuation FrameType = iota
	Text
	Binary
	Close = 8
	Ping
	Pong
)

func Write(w io.Writer, typ FrameType, p []byte) (n int, err error) {
	panic("TODO")
}

func Read(r io.Reader) (typ FrameType, payload io.Reader, err error) {
	panic("TODO")
}

func SecWebsocketAccept(secWebsocketKey string) string {
	panic("TODO")
}
