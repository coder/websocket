//go:build !js

package websocket

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/coder/websocket/internal/test/assert"
)

// A ping that the peer sent before it saw our close frame must still be
// answered, and the close handshake must go on to read the peer's close frame.
func TestCloseAnswersPingBeforePeerClose(t *testing.T) {
	t.Parallel()

	serverSide, peerSide := net.Pipe()
	c := newConn(connConfig{
		rwc: serverSide,
		br:  bufio.NewReader(serverSide),
		bw:  bufio.NewWriter(serverSide),
	})

	type peerResult struct {
		gotPong      bool
		pongPayload  string
		closeWritten bool
		err          error
	}
	peerDone := make(chan peerResult, 1)
	go func() {
		var res peerResult
		defer func() { peerDone <- res }()
		br := bufio.NewReader(peerSide)
		bw := bufio.NewWriter(peerSide)
		buf := make([]byte, 8)

		readFrame := func() (header, []byte, error) {
			h, err := readFrameHeader(br, buf)
			if err != nil {
				return h, nil, err
			}
			p := make([]byte, h.payloadLength)
			_, err = io.ReadFull(br, p)
			return h, p, err
		}
		writeFrame := func(op opcode, p []byte) error {
			// Client frames must be masked; a zero key leaves the payload as is.
			h := header{fin: true, opcode: op, payloadLength: int64(len(p)), masked: true}
			if err := writeFrameHeader(h, bw, buf); err != nil {
				return err
			}
			if _, err := bw.Write(p); err != nil {
				return err
			}
			return bw.Flush()
		}

		// Our close frame arrives first.
		h, _, err := readFrame()
		if err != nil || h.opcode != opClose {
			res.err = err
			return
		}
		// The peer had already queued a ping before it read our close.
		if res.err = writeFrame(opPing, []byte("hi")); res.err != nil {
			return
		}
		h, p, err := readFrame()
		if err == nil && h.opcode == opPong {
			res.gotPong = true
			res.pongPayload = string(p)
		}
		closePayload := make([]byte, 2)
		binary.BigEndian.PutUint16(closePayload, uint16(StatusNormalClosure))
		res.closeWritten = writeFrame(opClose, closePayload) == nil
	}()

	err := c.Close(StatusNormalClosure, "")
	assert.Success(t, err)

	select {
	case res := <-peerDone:
		assert.Success(t, res.err)
		assert.Equal(t, "got pong", true, res.gotPong)
		assert.Equal(t, "pong payload", "hi", res.pongPayload)
		assert.Equal(t, "peer close written", true, res.closeWritten)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for peer")
	}
}
