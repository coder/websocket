package wsjson_test

import (
	"encoding/json"
	"io"
	"strconv"
	"testing"

	"nhooyr.io/websocket/internal/test/xrand"
)

func BenchmarkJSON(b *testing.B) {
	sizes := []int{
		8,
		16,
		32,
		128,
		256,
		512,
		1024,
		2048,
		4096,
		8192,
		16384,
	}

	b.Run("json.Encoder", func(b *testing.B) {
		for _, size := range sizes {
			b.Run(strconv.Itoa(size), func(b *testing.B) {
				msg := xrand.String(size)
				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					json.NewEncoder(io.Discard).Encode(msg)
				}
			})
		}
	})
	b.Run("json.Marshal", func(b *testing.B) {
		for _, size := range sizes {
			b.Run(strconv.Itoa(size), func(b *testing.B) {
				msg := xrand.String(size)
				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					json.Marshal(msg)
				}
			})
		}
	})
}
