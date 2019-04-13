package websocket

import (
	"math"
	"strings"
	"testing"
)

func TestCloseError(t *testing.T) {
	t.Parallel()

	// Other parts of close error are tested by websocket_test.go right now
	// with the autobahn tests.

	testCases := []struct {
		name    string
		ce      CloseError
		success bool
	}{
		{
			name: "normal",
			ce: CloseError{
				Code:   StatusNormalClosure,
				Reason: strings.Repeat("x", maxControlFramePayload-2),
			},
			success: true,
		},
		{
			name: "bigReason",
			ce: CloseError{
				Code:   StatusNormalClosure,
				Reason: strings.Repeat("x", maxControlFramePayload-1),
			},
			success: false,
		},
		{
			name: "bigCode",
			ce: CloseError{
				Code:   math.MaxUint16,
				Reason: strings.Repeat("x", maxControlFramePayload-2),
			},
			success: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := tc.ce.bytes()
			if (err == nil) != tc.success {
				t.Fatalf("unexpected error value: %v", err)
			}
		})
	}
}
