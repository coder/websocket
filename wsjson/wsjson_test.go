package wsjson_test

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func BenchmarkJSON(b *testing.B) {
	msg := []byte(strings.Repeat("1234", 128))
	b.SetBytes(int64(len(msg)))
	b.ReportAllocs()
	b.Run("json.Encoder", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			json.NewEncoder(io.Discard).Encode(msg)
		}
	})
	b.Run("json.Marshal", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			json.Marshal(msg)
		}
	})
}
