// +build !js

package websocket_test

import (
	"context"
	"log"
	"net/http"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// This example accepts a WebSocket connection, reads a single JSON
// message from the client and then closes the connection.
func ExampleAccept() {
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			log.Println(err)
			return
		}
		defer c.Close(websocket.StatusInternalError, "the sky is falling")

		ctx, cancel := context.WithTimeout(r.Context(), time.Second*10)
		defer cancel()

		var v interface{}
		err = wsjson.Read(ctx, c, &v)
		if err != nil {
			log.Println(err)
			return
		}

		c.Close(websocket.StatusNormalClosure, "")
	})

	err := http.ListenAndServe("localhost:8080", fn)
	log.Fatal(err)
}

// This example dials a server, writes a single JSON message and then
// closes the connection.
func ExampleDial() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c, _, err := websocket.Dial(ctx, "ws://localhost:8080", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close(websocket.StatusInternalError, "the sky is falling")

	err = wsjson.Write(ctx, c, "hi")
	if err != nil {
		log.Fatal(err)
	}

	c.Close(websocket.StatusNormalClosure, "")
}

// This example dials a server and then expects to be disconnected with status code
// websocket.StatusNormalClosure.
func ExampleCloseStatus() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c, _, err := websocket.Dial(ctx, "ws://localhost:8080", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close(websocket.StatusInternalError, "the sky is falling")

	_, _, err = c.Reader(ctx)
	if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		log.Fatalf("expected to be disconnected with StatusNormalClosure but got: %+v", err)
		return
	}
}

// This example shows how to correctly handle a WebSocket connection
// on which you will only write and do not expect to read data messages.
func Example_writeOnly() {
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			log.Println(err)
			return
		}
		defer c.Close(websocket.StatusInternalError, "the sky is falling")

		ctx, cancel := context.WithTimeout(r.Context(), time.Minute*10)
		defer cancel()

		ctx = c.CloseRead(ctx)

		t := time.NewTicker(time.Second * 30)
		defer t.Stop()

		for {
			select {
			case <-ctx.Done():
				c.Close(websocket.StatusNormalClosure, "")
				return
			case <-t.C:
				err = wsjson.Write(ctx, c, "hi")
				if err != nil {
					log.Println(err)
					return
				}
			}
		}
	})

	err := http.ListenAndServe("localhost:8080", fn)
	log.Fatal(err)
}
