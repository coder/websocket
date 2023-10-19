package websocket_test

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

func TestMain(m *testing.M) {
	code := m.Run()
	if runtime.NumGoroutine() != 1 {
		fmt.Fprintf(os.Stderr, "goroutine leak detected, expected 1 but got %d goroutines\n", runtime.NumGoroutine())
		os.Exit(1)
	}
	os.Exit(code)
}
