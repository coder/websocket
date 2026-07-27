//go:build !js

package websocket_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/internal/test/assert"
	"github.com/coder/websocket/internal/test/wstest"
)

// drainWriteDeadline attaches a peer that consumes messages and returns a
// function that stops it.
//
// CloseRead is not usable for these tests: it closes the connection when a
// data message arrives, which is exactly what they send.
func drainWriteDeadline(c *websocket.Conn) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := c.Read(context.Background()); err != nil {
				return
			}
		}
	}()
	return func() {
		c.CloseNow()
		<-done
	}
}

func TestSetWriteDeadline(t *testing.T) {
	t.Parallel()

	msg := []byte("hello")

	// A deadline in the future does not get in the way, and clearing it with
	// the zero time puts things back as they were.
	t.Run("live and cleared", func(t *testing.T) {
		t.Parallel()

		c1, c2 := wstest.Pipe(nil, nil)
		defer c1.CloseNow()
		defer c2.CloseNow()
		defer drainWriteDeadline(c2)()

		assert.Success(t, c1.SetWriteDeadline(time.Now().Add(time.Minute)))
		assert.Success(t, c1.Write(context.Background(), websocket.MessageText, msg))

		assert.Success(t, c1.SetWriteDeadline(time.Time{}))
		assert.Success(t, c1.Write(context.Background(), websocket.MessageText, msg))
	})

	// A deadline that has already passed closes the connection rather than
	// writing, which is what an expired context passed to Write does.
	t.Run("expired", func(t *testing.T) {
		t.Parallel()

		c1, c2 := wstest.Pipe(nil, nil)
		defer c1.CloseNow()
		defer c2.CloseNow()
		defer drainWriteDeadline(c2)()

		assert.Success(t, c1.SetWriteDeadline(time.Now().Add(-time.Second)))

		err := c1.Write(context.Background(), websocket.MessageText, msg)
		if err == nil {
			t.Fatal("write succeeded past its deadline")
		}
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("write past its deadline failed with %v, want os.ErrDeadlineExceeded", err)
		}

		// And the connection goes away, as it does when a context passed to
		// Write expires. That happens on the deadline timer's goroutine
		// rather than inline, so give it a moment.
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, _, err := c1.Read(context.Background()); err != nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("connection still alive after its write deadline passed")
			}
		}
	})
}

// TestSetWriteDeadlineAllocs is the point of the method: a deadline set this
// way costs nothing per frame, where bounding a write with a context
// allocates one in the caller and a context.AfterFunc in here.
//
// It is deliberately not parallel — AllocsPerRun panics inside a parallel
// test.
func TestSetWriteDeadlineAllocs(t *testing.T) {
	msg := []byte("hello")

	c1, c2 := wstest.Pipe(nil, nil)
	defer c1.CloseNow()
	defer c2.CloseNow()
	defer drainWriteDeadline(c2)()

	write := func() {
		if err := c1.Write(context.Background(), websocket.MessageText, msg); err != nil {
			t.Fatal(err)
		}
	}

	// Compared against the same write with no deadline set, so this asserts
	// what the method claims — that a deadline is free — rather than that a
	// write allocates nothing, which is not this method's business.
	assert.Success(t, c1.SetWriteDeadline(time.Time{}))
	write() // warm any one-off buffers before counting
	without := testing.AllocsPerRun(200, write)

	assert.Success(t, c1.SetWriteDeadline(time.Now().Add(time.Hour)))
	with := testing.AllocsPerRun(200, write)

	if with > without {
		t.Fatalf("write with a deadline allocated %v, without a deadline %v; a deadline should be free", with, without)
	}
	t.Logf("allocations per write: %v without a deadline, %v with one", without, with)
}
