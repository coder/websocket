// +build !js

package websocket

import (
	"io"
	"math"
	"strings"
	"testing"

	"nhooyr.io/websocket/internal/test/cmp"
)

func TestCloseError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		ce      CloseError
		success bool
	}{
		{
			name: "normal",
			ce: CloseError{
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
			success: false,
		},
		{
			name: "bigCode",
			ce: CloseError{
				Code:   math.MaxUint16,
				Reason: strings.Repeat("x", maxCloseReason),
			},
			success: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := tc.ce.bytesErr()
			if tc.success != (err == nil) {
				t.Fatalf("unexpected error value (wanted err == nil == %v): %v", tc.success, err)
			}
		})
	}

	t.Run("Error", func(t *testing.T) {
		exp := `status = StatusInternalError and reason = "meow"`
		act := CloseError{
			Code:   StatusInternalError,
			Reason: "meow",
		}.Error()

		if (act) != exp {
			t.Fatal(cmp.Diff(exp, act))
		}
	})
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
				if err != nil {
					t.Fatal(err)
				}
				if !cmp.Equal(tc.ce, ce) {
					t.Fatalf("expected %v but got %v", tc.ce, ce)
				}
			} else if err == nil {
				t.Errorf("expected error: %v %v", ce, err)
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
			if !cmp.Equal(tc.valid, act) {
				t.Fatalf("unexpected valid: %v", cmp.Diff(tc.valid, act))
			}
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
			if !cmp.Equal(tc.exp, act) {
				t.Fatalf("unexpected closeStatus: %v", cmp.Diff(tc.exp, act))
			}
		})
	}
}
