package ws

import (
	"bufio"
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

func Writer(w bufio.Writer, typ FrameType) io.WriteCloser {
	panic("TODO")
}

func Read(r io.Reader) (typ FrameType, payload io.Reader, err error) {
	panic("TODO")
}

func SecWebsocketAccept(secWebsocketKey string) string {
	panic("TODO")
}
