package bpool

import (
	"bytes"
	"sync"
)

var bpool sync.Pool

// Get returns a buffer from the pool or creates a new one if
// the pool is empty.
func Get() *bytes.Buffer {
	b, ok := bpool.Get().(*bytes.Buffer)
	if !ok {
		b = &bytes.Buffer{}
	}
	return b
}

// Put returns a buffer into the pool.
func Put(b *bytes.Buffer) {
	b.Reset()
	bpool.Put(b)
}
