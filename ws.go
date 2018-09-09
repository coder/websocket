package ws

import (
	"bufio"
	"io"
	"net"
	"net/http"
)

func Writer(w bufio.Writer, typ Opcode) io.WriteCloser {
	panic("TODO")
}

func WriteFrame(w io.Writer, typ Opcode, p []byte) error {
	panic("TODO")
}

func ReadFrame(r io.Reader) (typ Opcode, payload io.Reader, err error) {
	panic("TODO")
}

func Upgrade(w http.ResponseWriter, r *http.Request) (net.Conn, *bufio.ReadWriter, error) {
	panic("TODO")
}
