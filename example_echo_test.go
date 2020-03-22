// +build !js

package websocket_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// TODO IMPROVE CANCELLATION AND SHUTDOWN
// TODO on context cancel send websocket going away and fix the read timeout error to be dependant on context deadline reached.
// TODO this way you cancel your context and the right message automatically gets sent. Furthrmore, then u can just use a simple waitgroup to wait for connections.
// TODO grace is wrong as it doesn't wait for the individual goroutines.

// This example starts a WebSocket echo server,
// dials the server and then sends 5 different messages
// and prints out the server's responses.
func Example_echo() {
	// First we listen on port 0 which means the OS will
	// assign us a random free port. This is the listener
	// the server will serve on and the client will connect to.
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer l.Close()

	var g websocket.Grace
	defer g.Close()
	s := &http.Server{
		Handler: g.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			err := echoServer(w, r)
			if err != nil {
				log.Printf("echo server: %v", err)
			}
		})),
		ReadTimeout:  time.Second * 15,
		WriteTimeout: time.Second * 15,
	}
	defer s.Close()

	// This starts the echo server on the listener.
	go func() {
		err := s.Serve(l)
		if err != http.ErrServerClosed {
			log.Fatalf("failed to listen and serve: %v", err)
		}
	}()

	// Now we dial the server, send the messages and echo the responses.
	err = client("ws://" + l.Addr().String())
	if err != nil {
		log.Fatalf("client failed: %v", err)
	}
	// Output:
	// received: map[i:0]
	// received: map[i:1]
	// received: map[i:2]
	// received: map[i:3]
	// received: map[i:4]
}

// echoServer is the WebSocket echo server implementation.
// It ensures the client speaks the echo subprotocol and
// only allows one message every 100ms with a 10 message burst.
func echoServer(w http.ResponseWriter, r *http.Request) error {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{"echo"},
	})
	if err != nil {
		return err
	}
	defer c.Close(websocket.StatusInternalError, "the sky is falling")

	if c.Subprotocol() != "echo" {
		c.Close(websocket.StatusPolicyViolation, "client must speak the echo subprotocol")
		return errors.New("client does not speak echo sub protocol")
	}

	l := rate.NewLimiter(rate.Every(time.Millisecond*100), 10)
	for {
		err = echo(r.Context(), c, l)
		if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to echo with %v: %w", r.RemoteAddr, err)
		}
	}
}

// echo reads from the WebSocket connection and then writes
// the received message back to it.
// The entire function has 10s to complete.
func echo(ctx context.Context, c *websocket.Conn, l *rate.Limiter) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	err := l.Wait(ctx)
	if err != nil {
		return err
	}

	typ, r, err := c.Reader(ctx)
	if err != nil {
		return err
	}

	w, err := c.Writer(ctx, typ)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, r)
	if err != nil {
		return fmt.Errorf("failed to io.Copy: %w", err)
	}

	err = w.Close()
	return err
}

// client dials the WebSocket echo server at the given url.
// It then sends it 5 different messages and echo's the server's
// response to each.
func client(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		Subprotocols: []string{"echo"},
	})
	if err != nil {
		return err
	}
	defer c.Close(websocket.StatusInternalError, "the sky is falling")

	for i := 0; i < 5; i++ {
		err = wsjson.Write(ctx, c, map[string]int{
			"i": i,
		})
		if err != nil {
			return err
		}

		v := map[string]int{}
		err = wsjson.Read(ctx, c, &v)
		if err != nil {
			return err
		}

		fmt.Printf("received: %v\n", v)
	}

	c.Close(websocket.StatusNormalClosure, "")
	return nil
}
