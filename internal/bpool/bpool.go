package bpool

import (
	"bytes"
	"sync"
)

var pool sync.Pool

// Get returns a buffer from the pool or creates a new one if
// the pool is empty.
func Get() *bytes.Buffer {
	b := pool.Get()
	if b == nil {
		return &bytes.Buffer{}
	}
	return b.(*bytes.Buffer)
}

// Put returns a buffer into the pool.
func Put(b *bytes.Buffer) {
	b.Reset()
	pool.Put(b)
}
