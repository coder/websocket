// +build !js

package wsecho

import (
	"context"
	"io"
	"log"
	"net/http"
	"time"

	"nhooyr.io/websocket"
)

// Serve provides a streaming WebSocket echo server
// for use in tests.
func Serve(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:       []string{"echo"},
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("echo server: failed to accept: %+v", err)
		return
	}
	defer c.Close(websocket.StatusInternalError, "")

	Loop(r.Context(), c)
}

// Loop echos every msg received from c until an error
// occurs or the context expires.
// The read limit is set to 1 << 30.
func Loop(ctx context.Context, c *websocket.Conn) {
	defer c.Close(websocket.StatusInternalError, "")

	c.SetReadLimit(1 << 30)

	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	b := make([]byte, 32<<10)
	echo := func() error {
		typ, r, err := c.Reader(ctx)
		if err != nil {
			return err
		}

		w, err := c.Writer(ctx, typ)
		if err != nil {
			return err
		}

		_, err = io.CopyBuffer(w, r, b)
		if err != nil {
			return err
		}

		err = w.Close()
		if err != nil {
			return err
		}

		return nil
	}

	for {
		err := echo()
		if err != nil {
			return
		}
	}
}
