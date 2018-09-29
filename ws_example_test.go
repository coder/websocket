package ws_test

import (
	"context"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"reflect"
	"time"

	"golang.org/x/time/rate"
	"nhooyr.io/ws"
	"nhooyr.io/ws/wsjson"
	"nhooyr.io/ws/wsutil"
)

func ExampleClient() {
	req, err := http.NewRequest(http.MethodGet, "https://demos.kaazing.com/echo", nil)
	if err != nil {
		log.Fatalf("failed to create valid HTTP request: %v", err)
	}

	ctx, cancel := context.WithTimeout(req.Context(), time.Second*30)
	defer cancel()

	d := net.Dialer{}
	netConn, err := d.DialContext(ctx, "tcp", req.Host)
	if err != nil {
		log.Fatalf("failed to dial %v: %v", req.Host, err)
	}

	netConn.SetDeadline(time.Now().Add(time.Minute))

	req = req.WithContext(ctx)
	c, resp, err := ws.ClientHandshake(req, netConn)
	if err != nil {
		log.Fatalf("failed to do websocket client handshake: %v; resp: %v", err, resp)
	}
	defer c.Close(ws.StatusInternalError, nil)

	for i := 0; i < 5; i++ {
		msg := map[string]interface{}{
			"foo": rand.Int(),
		}

		err = wsjson.WriteMessage(c, msg)
		if err != nil {
			log.Fatalf("failed to write msg: %v", err)
		}

		var readMsg map[string]interface{}
		err = wsjson.ReadMessage(c, &readMsg)
		if err != nil {
			log.Fatalf("failed to read msg: %v", err)
		}

		if !reflect.DeepEqual(msg, readMsg) {
			log.Fatalf("expected %v; got %v", msg, readMsg)
		}
	}

	err = c.Close(ws.StatusNormalClosure, nil)
	if err != nil {
		log.Fatalf("failed to write status normal closure.")
	}
}

func ExampleServer() {
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := wsutil.VerifyOrigin(w, r, r.Host)
		if err != nil {
			return
		}

		conn, err := ws.ServerHandshake(w, r)
		if err != nil {
			log.Printf("failed to upgrade HTTP to WebSocket: %v", err)
			return
		}
		defer conn.Close(ws.StatusInternalError, nil)

		ctx := context.Background()

		l := rate.NewLimiter(rate.Every(time.Millisecond*100), 500)
		for {
			ctx, cancel := context.WithTimeout(ctx, time.Hour*2)
			defer cancel()

			err = l.Wait(ctx)
			if err != nil {
				log.Printf("failed to wait for limiter to allow reading the next message: %v", err)
				return
			}

			typ, wsr, err := conn.ReadMessage(ctx)
			if err != nil {
				log.Printf("failed to read message: %v", err)
				return
			}

			ctx, cancel = context.WithTimeout(ctx, time.Second*10)
			defer cancel()

			wsr.Limit(16384)
			wsr.SetContext(ctx)

			wsw := conn.MessageWriter(typ)
			wsw.SetContext(ctx)

			_, err = io.Copy(wsw, wsr)
			if err != nil {
				log.Printf("failed to copy message: %v", err)
				return
			}

			err = wsw.Close()
			if err != nil {
				log.Printf("failed to close writer: %v", err)
				return
			}
		}
	})

	s := http.Server{
		Handler:      fn,
		ReadTimeout:  time.Second * 15,
		WriteTimeout: time.Second * 15,
	}

	err := s.ListenAndServe()
	if err != nil {
		log.Fatalf("failed to listen and serve: %v", err)
	}
}
