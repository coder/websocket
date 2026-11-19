package websocket_test

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"
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

	want := 1
	if runtime.GOOS == "js" {
		want = 2
	}
	// A goroutine can still exist after the call it serves has returned, until
	// the scheduler runs it to its end. The watchCtx goroutine of os/exec does
	// this after Cmd.Wait returns. The wait also lets through a leaked
	// goroutine that a timer ends within 5 seconds.
	got := runtime.NumGoroutine()
	for deadline := time.Now().Add(5 * time.Second); got != want && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
		got = runtime.NumGoroutine()
	}
	if got != want {
		fmt.Fprintf(os.Stderr, "goroutine leak detected, expected %d but got %d goroutines\n", want, got)
		fmt.Fprintf(os.Stderr, "%s\n", goroutineStacks())
		os.Exit(1)
	}
	os.Exit(code)
}
