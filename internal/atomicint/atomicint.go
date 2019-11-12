package atomicint

import (
	"fmt"
	"sync/atomic"
)

// See https://github.com/nhooyr/websocket/issues/153
type Int64 struct {
	v int64
}

func (v *Int64) Load() int64 {
	return atomic.LoadInt64(&v.v)
}

func (v *Int64) Store(i int64) {
	atomic.StoreInt64(&v.v, i)
}

func (v *Int64) String() string {
	return fmt.Sprint(v.Load())
}

// Increment increments the value and returns the new value.
func (v *Int64) Increment(delta int64) int64 {
	return atomic.AddInt64(&v.v, delta)
}

func (v *Int64) CAS(old, new int64) (swapped bool) {
	return atomic.CompareAndSwapInt64(&v.v, old, new)
}
