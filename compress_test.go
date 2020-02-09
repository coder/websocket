// +build !js

package websocket

import (
	"strings"
	"testing"

	"nhooyr.io/websocket/internal/test/xrand"
)

func Test_slidingWindow(t *testing.T) {
	t.Parallel()

	const testCount = 99
	const maxWindow = 99999
	for i := 0; i < testCount; i++ {
		t.Run("", func(t *testing.T) {
			t.Parallel()

			input := xrand.String(maxWindow)
			windowLength := xrand.Int(maxWindow)
			r := newSlidingWindow(windowLength)
			r.write([]byte(input))

			if cap(r.buf) != windowLength {
				t.Fatalf("sliding window length changed somehow: %q and windowLength %d", input, windowLength)
			}

			if !strings.HasSuffix(input, string(r.buf)) {
				t.Fatalf("r.buf is not a suffix of input: %q and %q", input, r.buf)
			}
		})
	}
}
