package ws_test

import (
	"context"
	"io"
	"log"
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"nhooyr.io/ws"
	"nhooyr.io/ws/wsjson"
)

func ExampleAccept_echo() {
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := ws.Accept(w, r,
			ws.AcceptSubprotocols("echo"),
		)
		if err != nil {
			log.Printf("server handshake failed: %v", err)
			return
		}
		defer c.Close(ws.StatusInternalError, "")

		ctx := context.Background()

		echo := func() error {
			ctx, cancel := context.WithTimeout(ctx, time.Minute)
			defer cancel()

			typ, r, err := c.ReadMessage(ctx)
			if err != nil {
				return err
			}

			ctx, cancel = context.WithTimeout(ctx, time.Second*10)
			defer cancel()

			r.SetContext(ctx)
			r.Limit(32768)

			w := c.MessageWriter(typ)
			w.SetContext(ctx)
			_, err = io.Copy(w, r)
			if err != nil {
				return err
			}

			err = w.Close()
			if err != nil {
				return err
			}

			return nil
		}

		l := rate.NewLimiter(rate.Every(time.Millisecond*100), 10)
		for l.Allow() {
			err := echo()
			if err != nil {
				log.Printf("failed to read message: %v", err)
				return
			}
		}
	})
	// For production deployments, use a net/http.Server configured
	// with the appropriate timeouts.
	err := http.ListenAndServe("localhost:8080", fn)
	if err != nil {
		log.Fatalf("failed to listen and serve: %v", err)
	}
}

func ExampleAccept() {
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := ws.Accept(w, r)
		if err != nil {
			log.Printf("server handshake failed: %v", err)
			return
		}
		defer c.Close(ws.StatusInternalError, "")

		ctx, cancel := context.WithTimeout(r.Context(), time.Second*10)
		defer cancel()

		err = wsjson.Write(ctx, c, map[string]interface{}{
			"my_field": "foo",
		})
		if err != nil {
			log.Printf("failed to write json struct: %v", err)
			return
		}

		c.Close(ws.StatusNormalClosure, "")
	})
	// For production deployments, use a net/http.Server configured
	// with the appropriate timeouts.
	err := http.ListenAndServe("localhost:8080", fn)
	if err != nil {
		log.Fatalf("failed to listen and serve: %v", err)
	}
}

func ExampleDial() {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	c, _, err := ws.Dial(ctx, "ws://localhost:8080")
	if err != nil {
		log.Fatalf("failed to ws dial: %v", err)
	}
	defer c.Close(ws.StatusInternalError, "")

	err = wsjson.Write(ctx, c, map[string]interface{}{
		"my_field": "foo",
	})
	if err != nil {
		log.Fatalf("failed to write json struct: %v", err)
	}

	c.Close(ws.StatusNormalClosure, "")
}
