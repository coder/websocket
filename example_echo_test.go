package websocket_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"golang.org/x/time/rate"
	"golang.org/x/xerrors"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// main starts a WebSocket echo server and
// then dials the server and sends 5 different messages
// and prints out the server's responses.
func Example_echo() {
	// First we listen on port 0, that means the OS will
	// assign us a random free port. This is the listener
	// the server will serve on and the client will connect to.
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
		return
	}
	defer l.Close()

	s := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			err := echoServer(w, r)
			if err != nil {
				log.Printf("echo server: %v", err)
			}
		}),
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

	// Now we dial the server and send the messages.
	err = client("ws://" + l.Addr().String())
	if err != nil {
		log.Fatalf("client failed: %v", err)
	}

	// Output:
	// 0
	// 1
	// 2
	// 3
	// 4
}

// echoServer is the WebSocket echo server implementation.
// It ensures the client speaks the echo subprotocol and
// only allows one message every 100ms with a 10 message burst.
func echoServer(w http.ResponseWriter, r *http.Request) error {
	c, err := websocket.Accept(w, r, websocket.AcceptOptions{
		Subprotocols: []string{"echo"},
	})
	if err != nil {
		return err
	}
	defer c.Close(websocket.StatusInternalError, "")

	if c.Subprotocol() == "" {
		c.Close(websocket.StatusPolicyViolation, "cannot communicate with the default protocol")
		return xerrors.Errorf("client does not speak echo sub protocol")
	}

	ctx := r.Context()
	l := rate.NewLimiter(rate.Every(time.Millisecond*100), 10)

	for {
		err = echo(ctx, c, l)
		if err != nil {
			return xerrors.Errorf("failed to echo: %w", err)
		}
	}
}

// echo reads from the websocket connection and then writes
// the received message back to it.
// It only waits 1 minute to read and write the message and
// limits the received message to 32768 bytes.
func echo(ctx context.Context, c *websocket.Conn, l *rate.Limiter) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	err := l.Wait(ctx)
	if err != nil {
		return err
	}

	typ, r, err := c.Reader(ctx)
	if err != nil {
		return err
	}
	r = io.LimitReader(r, 32768)

	w, err := c.Writer(ctx, typ)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, r)
	if err != nil {
		return err
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

	c, _, err := websocket.Dial(ctx, url, websocket.DialOptions{
		Subprotocols: []string{"echo"},
	})
	if err != nil {
		return err
	}
	defer c.Close(websocket.StatusInternalError, "")

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

		fmt.Printf("%v\n", v["i"])
	}

	c.Close(websocket.StatusNormalClosure, "")
	return nil
}
