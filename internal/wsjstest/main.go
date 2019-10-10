// +build !js

package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/wsecho"
	"nhooyr.io/websocket/internal/wsgrace"
)

func main() {
	log.SetPrefix("wsecho")

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols:       []string{"echo"},
			InsecureSkipVerify: true,
		})
		if err != nil {
			log.Fatalf("echo server: failed to accept: %+v", err)
		}
		defer c.Close(websocket.StatusInternalError, "")

		err = wsecho.Loop(r.Context(), c)
		if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
			log.Fatalf("unexpected echo loop error: %+v", err)
		}
	}))
	closeFn := wsgrace.Grace(s.Config)
	defer func() {
		err := closeFn()
		if err != nil {
			log.Fatal(err)
		}
	}()

	wsURL := strings.Replace(s.URL, "http", "ws", 1)
	fmt.Printf("%v\n", wsURL)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)

	<-sigs
}
