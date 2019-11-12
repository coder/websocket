package bufpool

import (
	"strconv"
	"sync"
	"testing"
)

func BenchmarkSyncPool(b *testing.B) {
	sizes := []int{
		2,
		16,
		32,
		64,
		128,
		256,
		512,
		4096,
		16384,
	}
	for _, size := range sizes {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			b.Run("allocate", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					buf := make([]byte, size)
					_ = buf
				}
			})
			b.Run("pool", func(b *testing.B) {
				b.ReportAllocs()

				p := sync.Pool{}

				for i := 0; i < b.N; i++ {
					buf := p.Get()
					if buf == nil {
						buf = make([]byte, size)
					}

					p.Put(buf)
				}
			})
		})
	}
}
