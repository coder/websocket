package xrand

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// Bytes generates random bytes with length n.
func Bytes(n int) []byte {
	b := make([]byte, n)
	_, err := rand.Reader.Read(b)
	if err != nil {
		panic(fmt.Sprintf("failed to generate rand bytes: %v", err))
	}
	return b
}

// String generates a random string with length n.
func String(n int) string {
	s := strings.ToValidUTF8(string(Bytes(n)), "_")
	s = strings.ReplaceAll(s, "\x00", "_")
	if len(s) > n {
		return s[:n]
	}
	if len(s) < n {
		// Pad with =
		extra := n - len(s)
		return s + strings.Repeat("=", extra)
	}
	return s
}

// Bool returns a randomly generated boolean.
func Bool() bool {
	return Int(2) == 1
}

// Int returns a randomly generated integer between [0, max).
func Int(max int) int {
	x, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		panic(fmt.Sprintf("failed to get random int: %v", err))
	}
	return int(x.Int64())
}
