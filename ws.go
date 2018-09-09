package ws

import (
	"bufio"
	"io"
	"net"
	"net/http"

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

func Write(w io.Writer, typ FrameType, p []byte) error {
	panic("TODO")
}

func Read(r io.Reader) (typ FrameType, payload io.Reader, err error) {
	panic("TODO")
}

func Upgrade(w http.ResponseWriter, r *http.Request) (net.Conn, bufio.ReadWriter, error) {
	panic("TODO")
}
