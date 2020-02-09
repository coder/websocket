package websocket

import (
	"strings"
	"testing"

	"cdr.dev/slog/sloggers/slogtest/assert"

	"nhooyr.io/websocket/internal/test/xrand"
)

func Test_slidingWindow(t *testing.T) {
	t.Parallel()

	const testCount = 99
	const maxWindow = 99999
	for i := 0; i < testCount; i++ {
		input := xrand.String(maxWindow)
		windowLength := xrand.Int(maxWindow)
		r := newSlidingWindow(windowLength)
		r.write([]byte(input))

		if cap(r.buf) != windowLength {
			t.Fatalf("sliding window length changed somehow: %q and windowLength %d", input, windowLength)
		}
		assert.True(t, "hasSuffix", strings.HasSuffix(input, string(r.buf)))
	}
}
