package ws

import (
	"bufio"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
)

type Conn struct {
	ReadWriter io.ReadWriter
	Client     bool
}

func (c Conn) WriteFrame(op Opcode, p []byte) error {
	panic("TODO")
}

func (c Conn) StreamFrame(op Opcode) io.WriteCloser {
	panic("TODO")
}

func (c Conn) ReadFrame() (typ Opcode, payload io.Reader, err error) {
	panic("TODO")
}

func Upgrade(w http.ResponseWriter, r *http.Request) (net.Conn, *bufio.ReadWriter, error) {
	panic("TODO")
}

var secWebsocketKey = base64.StdEncoding.EncodeToString(make([]byte, 16))

func Handshake(w io.Writer, r *bufio.Reader, req *http.Request) (*http.Response, error) {
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", secWebsocketKey)

	// If w is a *bufio.Wgriter, req.Write will flush for us.
	err := req.Write(w)
	if err != nil {
		return nil, err
	}

	resp, err := http.ReadResponse(r, req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		return resp, errors.New("websocket handshake failed; see response")
	}

	return resp, nil
}
