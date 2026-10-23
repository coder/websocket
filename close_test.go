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

func Test_truncateCloseReason(t *testing.T) {
	t.Parallel()

	ascii := strings.Repeat("x", maxCloseReason)
	testCases := []struct {
		name   string
		reason string
		want   string
	}{
		{name: "empty", reason: "", want: ""},
		{name: "max", reason: ascii, want: ascii},
		{name: "maxPlusOne", reason: ascii + "x", want: ascii},
		{name: "twoByteFits", reason: ascii[:121] + "é", want: ascii[:121] + "é"},
		{name: "twoByteSplit", reason: ascii[:122] + "é", want: ascii[:122]},
		{name: "threeByteAligned", reason: strings.Repeat("€", 42), want: strings.Repeat("€", 41)},
		{name: "threeByteSplit", reason: ascii[:122] + "€", want: ascii[:122]},
		{name: "fourByteSplit", reason: strings.Repeat("😀", 31), want: strings.Repeat("😀", 30)},
		// U+FFFD decodes as utf8.RuneError but is a valid 3 byte rune.
		{name: "replacementCharSplit", reason: ascii[:122] + "\uFFFD", want: ascii[:122]},
		// Invalid UTF-8 is cut at the limit, not scanned for a boundary.
		{name: "continuationBytes", reason: strings.Repeat("\x80", 200), want: strings.Repeat("\x80", maxCloseReason)},
		{name: "incompleteRune", reason: ascii[:122] + "\xe2\x82x", want: ascii[:122] + "\xe2"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := truncateCloseReason(tc.reason)
			assert.Equal(t, "reason", tc.want, got)
			if got == tc.reason {
				assert.Success(t, err)
			} else {
				assert.ErrorIs(t, ErrCloseReasonTruncated, err)
			}
			if utf8.ValidString(tc.reason) && !utf8.ValidString(got) {
				t.Fatalf("truncated valid UTF-8 into invalid UTF-8: %q", got)
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
