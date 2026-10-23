//go:build !js

package websocket

import (
	"io"
	"math"
	"strings"
	"testing"
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
		name      string
		reason    string
		want      string
		truncated string // Expected error message, empty if not truncated.
	}{
		{name: "empty", reason: "", want: ""},
		{name: "max", reason: ascii, want: ascii},
		{name: "maxPlusOne", reason: ascii + "x", want: ascii, truncated: "from 124 to 123 bytes"},
		{name: "twoByteFits", reason: ascii[:121] + "é", want: ascii[:121] + "é"},
		{name: "twoByteSplit", reason: ascii[:122] + "é", want: ascii[:122], truncated: "from 124 to 122 bytes"},
		{name: "threeByteAligned", reason: strings.Repeat("€", 42), want: strings.Repeat("€", 41), truncated: "from 126 to 123 bytes"},
		{name: "threeByteSplit", reason: ascii[:122] + "€", want: ascii[:122], truncated: "from 125 to 122 bytes"},
		{name: "fourByteSplit", reason: strings.Repeat("😀", 31), want: strings.Repeat("😀", 30), truncated: "from 124 to 120 bytes"},
		// U+FFFD decodes as utf8.RuneError but is a valid 3 byte rune.
		{name: "replacementCharSplit", reason: ascii[:122] + "\uFFFD", want: ascii[:122], truncated: "from 125 to 122 bytes"},
		{name: "invalidByte", reason: "a\x80b", want: "a\uFFFDb"},
		{name: "invalidRun", reason: "a\x80\x80\xffb", want: "a\uFFFDb"},
		{name: "incompleteRune", reason: "a\xe2\x82", want: "a\uFFFD"},
		{name: "invalidRunOverMax", reason: strings.Repeat("\x80", 200), want: "\uFFFD"},
		// Replacement grows these past maxCloseReason.
		{name: "growthFromMax", reason: ascii[:122] + "\x80", want: ascii[:122], truncated: "from 123 to 122 bytes"},
		{name: "growthIsolated", reason: strings.Repeat("\x80x", 61), want: strings.Repeat("\uFFFDx", 30) + "\uFFFD", truncated: "from 122 to 123 bytes"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := wireCloseReason(tc.reason)
			assert.Equal(t, "reason", tc.want, got)
			if tc.truncated == "" {
				assert.Success(t, err)
			} else {
				assert.ErrorIs(t, ErrCloseReasonTruncated, err)
				assert.Equal(t, "error", "websocket: close reason truncated "+tc.truncated, err.Error())
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
