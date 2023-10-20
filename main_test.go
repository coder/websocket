package websocket_test

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

func goroutineStacks() []byte {
	buf := make([]byte, 512)
	for {
		m := runtime.Stack(buf, true)
		if m < len(buf) {
			return buf[:m]
		}
		buf = make([]byte, len(buf)*2)
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	if runtime.GOOS != "js" && runtime.NumGoroutine() != 1 ||
		runtime.GOOS == "js" && runtime.NumGoroutine() != 2 {
		fmt.Fprintf(os.Stderr, "goroutine leak detected, expected 1 but got %d goroutines\n", runtime.NumGoroutine())
		fmt.Fprintf(os.Stderr, "%s\n", goroutineStacks())
		os.Exit(1)
	}
	os.Exit(code)
}
