// +build js

package wsjs

import (
	"context"
	"syscall/js"
	"testing"
	"time"
)

func TestWebSocket(t *testing.T) {
	t.Parallel()

	c, err := New(context.Background(), "ws://localhost:8081", nil)
	if err != nil {
		t.Fatal(err)
	}

	c.OnError(func(e js.Value) {
		t.Log(js.Global().Get("JSON").Call("stringify", e))
		t.Log(c.v.Get("readyState"))
	})

	time.Sleep(time.Second)
}
