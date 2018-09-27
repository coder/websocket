package ws_test

import (
	"context"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"time"

	"nhooyr.io/ws"
	"nhooyr.io/ws/wsjson"
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
	defer c.Close()

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

	deadline := time.Now().Add(time.Second * 15)
	_ = c.WriteCloseMessage(ws.StatusNormalClosure, nil, deadline)
}

func ExampleServer() {
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			url, err := url.Parse(origin)
			if err != nil {
				http.Error(w, "invalid origin header", http.StatusBadRequest)
				return
			}
			if url.Host != r.Host {
				http.Error(w, "unexpected origin host", http.StatusForbidden)
				return
			}
		}

		conn, err := ws.ServerHandshake(w, r)
		if err != nil {
			log.Printf("failed to upgrade HTTP to WebSocket: %v", err)
			return
		}
		defer conn.Close()

		for {
			typ, wsr, err := conn.ReadMessage()
			if err != nil {
				log.Printf("failed to read message: %v", err)
				return
			}

			wsr.SetLimit(16384)
			deadline := time.Now().Add(time.Second * 15)
			wsr.SetContext(deadline)

			wsw := conn.WriteMessage(typ)
			wsw.SetContext(deadline)

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
