package ws_test

import (
	"io"
	"log"
	"net/http"
	"time"

	"nhooyr.io/ws"
)

func ExampleConn() {
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := ws.ServerHandshake(w, r)
		if err != nil {
			log.Printf("failed to upgrade HTTP to WebSocket: %v", err)
			return
		}
		defer conn.Close()

		for {
			typ, wsr, err := conn.ReadDataMessage()
			if err != nil {
				log.Printf("failed to read message: %v", err)
				return
			}

			wsr.SetLimit(16384)
			deadline := time.Now().Add(time.Second * 15)
			wsr.SetDeadline(deadline)

			wsw := conn.WriteDataMessage(typ)
			wsw.SetDeadline(deadline)

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
