package websocket_test

import (
	"context"
	"io"
	"log"
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"nhooyr.io/websocket"
)

func ExampleAccept_echo() {
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r)
		if err != nil {
			log.Printf("server handshake failed: %v", err)
			return
		}
		defer c.Close(websocket.StatusInternalError, "")

		echo := func() error {
			ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
			defer cancel()

			typ, r, err := c.Read(ctx)
			if err != nil {
				return err
			}

			r = io.LimitReader(r, 32768)

			w := c.Write(ctx, typ)
			_, err = io.Copy(w, r)
			if err != nil {
				return err
			}

			err = w.Close()
			return err
		}

		l := rate.NewLimiter(rate.Every(time.Millisecond*100), 10)
		for l.Allow() {
			err := echo()
			if err != nil {
				log.Printf("failed to echo message: %v", err)
				return
			}
		}
	})

	s := &http.Server{
		Addr:         "localhost:8080",
		Handler:      fn,
		ReadTimeout:  time.Second * 15,
		WriteTimeout: time.Second * 15,
	}
	err := s.ListenAndServe()
	if err != nil {
		log.Fatalf("failed to listen and serve: %v", err)
	}
}

func ExampleAccept() {
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r)
		if err != nil {
			log.Printf("server handshake failed: %v", err)
			return
		}
		defer c.Close(websocket.StatusInternalError, "")

		jc := websocket.JSONConn{
			Conn: c,
		}

		ctx, cancel := context.WithTimeout(r.Context(), time.Second*10)
		defer cancel()

		v := map[string]interface{}{
			"my_field": "foo",
		}
		err = jc.Write(ctx, v)
		if err != nil {
			log.Printf("failed to write json: %v", err)
			return
		}

		log.Printf("wrote %v", v)

		c.Close(websocket.StatusNormalClosure, "")
	})
	err := http.ListenAndServe("localhost:8080", fn)
	if err != nil {
		log.Fatalf("failed to listen and serve: %v", err)
	}
}

func ExampleDial() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c, _, err := websocket.Dial(ctx, "ws://localhost:8080")
	if err != nil {
		log.Fatalf("failed to ws dial: %v", err)
	}
	defer c.Close(websocket.StatusInternalError, "")

	jc := websocket.JSONConn{
		Conn: c,
	}

	var v interface{}
	err = jc.Read(ctx, v)
	if err != nil {
		log.Fatalf("failed to read json: %v", err)
	}

	log.Printf("received %v", v)

	c.Close(websocket.StatusNormalClosure, "")
}
