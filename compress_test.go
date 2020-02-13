// +build !js

package websocket

import (
	"strings"
	"testing"

	"nhooyr.io/websocket/internal/test/assert"
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
			var sw slidingWindow
			sw.init(windowLength)
			sw.write([]byte(input))

			assert.Equal(t, "window length", windowLength, cap(sw.buf))
			if !strings.HasSuffix(input, string(sw.buf)) {
				t.Fatalf("r.buf is not a suffix of input: %q and %q", input, sw.buf)
			}
		})
	}
}
