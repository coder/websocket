//go:build !js

package websocket

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket/internal/test/assert"
)

func TestCloseError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		ce      CloseError
		want    CloseError
		success bool
	}{
		{
			name: "normal",
			ce: CloseError{
				Code:   StatusNormalClosure,
				Reason: strings.Repeat("x", maxCloseReason),
			},
			want: CloseError{
				Code:   StatusNormalClosure,
				Reason: strings.Repeat("x", maxCloseReason),
			},
			success: true,
		},
		{
			name: "bigReason",
			ce: CloseError{
				Code:   StatusNormalClosure,
				Reason: strings.Repeat("x", maxCloseReason+1),
			},
			want: CloseError{
				Code:   StatusNormalClosure,
				Reason: strings.Repeat("x", maxCloseReason),
			},
			success: false,
		},
		{
			name: "bigCode",
			ce: CloseError{
				Code:   math.MaxUint16,
				Reason: strings.Repeat("x", maxCloseReason),
			},
			want:    CloseError{Code: StatusInternalError},
			success: false,
		},
		{
			name: "noStatus",
			ce: CloseError{
				Code:   StatusNoStatusRcvd,
				Reason: "dropped",
			},
			want:    CloseError{Code: StatusNoStatusRcvd},
			success: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ce, err := wireCloseError(tc.ce.Code, tc.ce.Reason)
			if tc.success {
				assert.Success(t, err)
			} else {
				assert.Error(t, err)
			}
			assert.Equal(t, "wire close error", tc.want, ce)
		})
	}

	t.Run("Error", func(t *testing.T) {
		exp := `status = StatusInternalError and reason = "meow"`
		act := CloseError{
			Code:   StatusInternalError,
			Reason: "meow",
		}.Error()
		assert.Equal(t, "CloseError.Error()", exp, act)
	})
}

func Test_wireCloseReason(t *testing.T) {
	t.Parallel()

	ascii := strings.Repeat("x", maxCloseReason)
	testCases := []struct {
		name          string
		reason        string
		want          string
		wantErrSuffix string
	}{
		{name: "empty", reason: "", want: ""},
		{name: "max", reason: ascii, want: ascii},
		{name: "maxPlusOne", reason: ascii + "x", want: ascii, wantErrSuffix: "from 124 to 123 bytes"},
		{name: "twoByteFits", reason: ascii[:121] + "é", want: ascii[:121] + "é"},
		{name: "twoByteSplit", reason: ascii[:122] + "é", want: ascii[:122], wantErrSuffix: "from 124 to 122 bytes"},
		{name: "threeByteAligned", reason: strings.Repeat("€", 42), want: strings.Repeat("€", 41), wantErrSuffix: "from 126 to 123 bytes"},
		{name: "threeByteSplit", reason: ascii[:122] + "€", want: ascii[:122], wantErrSuffix: "from 125 to 122 bytes"},
		{name: "fourByteSplit", reason: strings.Repeat("😀", 31), want: strings.Repeat("😀", 30), wantErrSuffix: "from 124 to 120 bytes"},
		{name: "replacementCharSplit", reason: ascii[:122] + "\uFFFD", want: ascii[:122], wantErrSuffix: "from 125 to 122 bytes"},
		{name: "invalidByte", reason: "a\x80b", want: "a\uFFFDb"},
		{name: "invalidRun", reason: "a\x80\x80\xffb", want: "a\uFFFDb"},
		{name: "incompleteRune", reason: "a\xe2\x82", want: "a\uFFFD"},
		{name: "invalidRunOverMax", reason: strings.Repeat("\x80", 200), want: "\uFFFD"},
		{name: "growthFromMax", reason: ascii[:122] + "\x80", want: ascii[:122], wantErrSuffix: "from 125 to 122 bytes after replacing invalid UTF-8"},
		{name: "growthIsolated", reason: strings.Repeat("\x80x", 61), want: strings.Repeat("\uFFFDx", 30) + "\uFFFD", wantErrSuffix: "from 244 to 123 bytes after replacing invalid UTF-8"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := wireCloseReason(tc.reason)
			assert.Equal(t, "reason", tc.want, got)
			if tc.wantErrSuffix == "" {
				assert.Success(t, err)
			} else {
				assert.ErrorIs(t, ErrCloseReasonTruncated, err)
				assert.Equal(t, "error", "websocket: close reason truncated "+tc.wantErrSuffix, err.Error())
			}
			if !utf8.ValidString(got) || len(got) > maxCloseReason {
				t.Fatalf("reason %q is not valid UTF-8 within %d bytes", got, maxCloseReason)
			}
		})
	}
}

func Test_parseClosePayload(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		p       []byte
		success bool
		ce      CloseError
		err     string
	}{
		{
			name:    "normal",
			p:       append([]byte{0x3, 0xE8}, []byte("hello")...),
			success: true,
			ce: CloseError{
				Code:   StatusNormalClosure,
				Reason: "hello",
			},
		},
		{
			name:    "nothing",
			success: true,
			ce: CloseError{
				Code: StatusNoStatusRcvd,
			},
		},
		{
			name:    "oneByte",
			p:       []byte{0},
			success: false,
		},
		{
			name:    "badStatusCode",
			p:       []byte{0x17, 0x70},
			success: false,
			err:     "invalid close status code StatusCode(6000)",
		},
		{
			name:    "noStatusCode",
			p:       []byte{0x03, 0xED},
			success: false,
			err:     "invalid close status code StatusNoStatusRcvd",
		},
		{
			name:    "invalidUTF8Reason",
			p:       []byte{0x03, 0xE8, 0xFF, 'a'},
			success: true,
			ce:      CloseError{Code: StatusNormalClosure, Reason: "\uFFFDa"},
		},
		{
			// Each "\xFFa" becomes 4 bytes, so the reason is truncated.
			name:    "invalidUTF8ReasonTooLong",
			p:       append([]byte{0x03, 0xE8}, strings.Repeat("\xFFa", 61)+"a"...),
			success: true,
			ce:      CloseError{Code: StatusNormalClosure, Reason: strings.Repeat("\uFFFDa", 30) + "\uFFFD"},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ce, err := parseClosePayload(tc.p)
			if tc.success {
				assert.Success(t, err)
				assert.Equal(t, "close payload", tc.ce, ce)
			} else {
				assert.Error(t, err)
				if tc.err != "" {
					assert.Equal(t, "error", tc.err, err.Error())
				}
			}
		})
	}
}

func Test_validWireCloseCode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		code  StatusCode
		valid bool
	}{
		{
			name:  "normal",
			code:  StatusNormalClosure,
			valid: true,
		},
		{
			name:  "noStatus",
			code:  StatusNoStatusRcvd,
			valid: false,
		},
		{
			name:  "3000",
			code:  3000,
			valid: true,
		},
		{
			name:  "4999",
			code:  4999,
			valid: true,
		},
		{
			name:  "unknown",
			code:  5000,
			valid: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			act := validWireCloseCode(tc.code)
			assert.Equal(t, "wire close code", tc.valid, act)
		})
	}
}

func TestCloseStatus(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		in   error
		exp  StatusCode
	}{
		{
			name: "nil",
			in:   nil,
			exp:  -1,
		},
		{
			name: "io.EOF",
			in:   io.EOF,
			exp:  -1,
		},
		{
			name: "StatusInternalError",
			in: CloseError{
				Code: StatusInternalError,
			},
			exp: StatusInternalError,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			act := CloseStatus(tc.in)
			assert.Equal(t, "close status", tc.exp, act)
		})
	}
}

// A ping queued before the peer saw our close frame must still be answered
// with a pong, and the handshake must continue to the peer's close frame.
func TestClosePingAfterCloseSent(t *testing.T) {
	t.Parallel()

	serverSide, peerSide := net.Pipe()
	defer peerSide.Close()

	c := newConn(connConfig{
		rwc: serverSide,
		br:  bufio.NewReader(serverSide),
		bw:  bufio.NewWriter(serverSide),
	})

	type peerResult struct {
		gotPong     bool
		pongPayload string
		closeSent   bool
		err         error
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
			// Client frames must be masked; a zero mask key leaves
			// the payload unchanged.
			h := header{fin: true, opcode: op, payloadLength: int64(len(p)), masked: true}
			if err := writeFrameHeader(h, bw, buf); err != nil {
				return err
			}
			if _, err := bw.Write(p); err != nil {
				return err
			}
			return bw.Flush()
		}

		h, _, err := readFrame()
		if err != nil {
			res.err = err
			return
		}
		if h.opcode != opClose {
			res.err = fmt.Errorf("expected close frame, got %v", h.opcode)
			return
		}
		if err := writeFrame(opPing, []byte("hi")); err != nil {
			res.err = err
			return
		}
		h, p, err := readFrame()
		if err == nil && h.opcode == opPong {
			res.gotPong = true
			res.pongPayload = string(p)
		}
		res.closeSent = writeFrame(opClose, CloseError{Code: StatusNormalClosure}.bytes()) == nil
	}()

	err := c.Close(StatusNormalClosure, "")
	assert.Success(t, err)

	select {
	case res := <-peerDone:
		assert.Success(t, res.err)
		assert.Equal(t, "got pong", true, res.gotPong)
		assert.Equal(t, "pong payload", "hi", res.pongPayload)
		assert.Equal(t, "peer close sent", true, res.closeSent)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for peer")
	}
}
