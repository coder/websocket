package websocket

import (
	"crypto/rand"
	"encoding/base64"
	"math/big"
	"strings"
	"testing"

	"cdr.dev/slog/sloggers/slogtest/assert"
)

func Test_slidingWindow(t *testing.T) {
	t.Parallel()

	const testCount = 99
	const maxWindow = 99999
	for i := 0; i < testCount; i++ {
		input := randStr(t, maxWindow)
		windowLength := randInt(t, maxWindow)
		r := newSlidingWindow(windowLength)
		r.write([]byte(input))

		if cap(r.buf) != windowLength {
			t.Fatalf("sliding window length changed somehow: %q and windowLength %d", input, windowLength)
		}
		assert.True(t, "hasSuffix", strings.HasSuffix(input, string(r.buf)))
	}
}

func randStr(t *testing.T, max int) string {
	n := randInt(t, max)

	b := make([]byte, n)
	_, err := rand.Read(b)
	assert.Success(t, "rand.Read", err)

	return base64.StdEncoding.EncodeToString(b)
}

func randInt(t *testing.T, max int) int {
	x, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	assert.Success(t, "rand.Int", err)
	return int(x.Int64())
}
