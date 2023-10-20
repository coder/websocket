package websocket_test

import (
	"context"
	"log"
	"net/http"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

func ExampleAccept() {
	// This handler accepts a WebSocket connection, reads a single JSON
	// message from the client and then closes the connection.

	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			log.Println(err)
			return
		}
		defer c.CloseNow()

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

func ExampleDial() {
	// Dials a server, writes a single JSON message and then
	// closes the connection.

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c, _, err := websocket.Dial(ctx, "ws://localhost:8080", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer c.CloseNow()

	err = wsjson.Write(ctx, c, "hi")
	if err != nil {
		log.Fatal(err)
	}

	c.Close(websocket.StatusNormalClosure, "")
}

func ExampleCloseStatus() {
	// Dials a server and then expects to be disconnected with status code
	// websocket.StatusNormalClosure.

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c, _, err := websocket.Dial(ctx, "ws://localhost:8080", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer c.CloseNow()

	_, _, err = c.Reader(ctx)
	if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		log.Fatalf("expected to be disconnected with StatusNormalClosure but got: %v", err)
	}
}

func Example_writeOnly() {
	// This handler demonstrates how to correctly handle a write only WebSocket connection.
	// i.e you only expect to write messages and do not expect to read any messages.
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			log.Println(err)
			return
		}
		defer c.CloseNow()

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

func Example_crossOrigin() {
	// This handler demonstrates how to safely accept cross origin WebSockets
	// from the origin example.com.
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"example.com"},
		})
		if err != nil {
			log.Println(err)
			return
		}
		c.Close(websocket.StatusNormalClosure, "cross origin WebSocket accepted")
	})

	err := http.ListenAndServe("localhost:8080", fn)
	log.Fatal(err)
}

func ExampleConn_Ping() {
	// Dials a server and pings it 5 times.

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c, _, err := websocket.Dial(ctx, "ws://localhost:8080", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer c.CloseNow()

	// Required to read the Pongs from the server.
	ctx = c.CloseRead(ctx)

	for i := 0; i < 5; i++ {
		err = c.Ping(ctx)
		if err != nil {
			log.Fatal(err)
		}
	}

	c.Close(websocket.StatusNormalClosure, "")
}

// This example demonstrates full stack chat with an automated test.
func Example_fullStackChat() {
	// https://github.com/nhooyr/websocket/tree/master/internal/examples/chat
}

// This example demonstrates a echo server.
func Example_echo() {
	// https://github.com/nhooyr/websocket/tree/master/internal/examples/echo
}
