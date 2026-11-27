//go:build !js

package websocket

import (
	"io"
	"testing"

	"github.com/coder/websocket/internal/test/assert"
)

func TestBufioWriterPool(t *testing.T) {
	t.Parallel()

	for _, size := range []int{-1, 0, defaultWriteBufferSize, 5, 1024, 128 << 10} {
		bw := getBufioWriter(io.Discard, size)
		want := size
		if size <= 0 {
			want = defaultWriteBufferSize
		}
		assert.Equal(t, "size", want, bw.Size())
		putBufioWriter(bw)
	}

	// sync.Pool may drop a Put, so repeat to reliably catch a leaked writer.
	for range 100 {
		putBufioWriter(getBufioWriter(io.Discard, 128<<10))
		bw := getBufioWriter(io.Discard, 0)
		assert.Equal(t, "pooled size", defaultWriteBufferSize, bw.Size())
		putBufioWriter(bw)
	}
}
