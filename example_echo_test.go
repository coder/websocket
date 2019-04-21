package websocket_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"time"

	"golang.org/x/time/rate"
	"golang.org/x/xerrors"

	"nhooyr.io/websocket"
)

func Example_echo() {
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

	go func() {
		err := s.Serve(l)
		if err != http.ErrServerClosed {
			log.Fatalf("failed to listen and serve: %v", err)
		}
	}()

	err = client("ws://" + l.Addr().String())
	if err != nil {
		log.Fatalf("client failed: %v", err)
	}

	// Output:
	// {"i":0}
	// {"i":1}
	// {"i":2}
	// {"i":3}
	// {"i":4}
}

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
		w, err := c.Writer(ctx, websocket.MessageText)
		if err != nil {
			return err
		}

		e := json.NewEncoder(w)
		err = e.Encode(map[string]int{
			"i": i,
		})
		if err != nil {
			return err
		}

		err = w.Close()
		if err != nil {
			return err
		}

		typ, r, err := c.Reader(ctx)
		if err != nil {
			return err
		}

		if typ != websocket.MessageText {
			return xerrors.Errorf("expected text message but got %v", typ)
		}

		msg2, err := ioutil.ReadAll(r)
		if err != nil {
			return err
		}

		fmt.Printf("%s", msg2)
	}

	c.Close(websocket.StatusNormalClosure, "")
	return nil
}
