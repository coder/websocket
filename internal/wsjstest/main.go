// +build !js

package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/wsecho"
)

func main() {
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

		var ce websocket.CloseError
		if !errors.As(err, &ce) || ce.Code != websocket.StatusNormalClosure {
			log.Fatalf("unexpected loop error: %+v", err)
		}

		os.Exit(0)
	}))

	wsURL := strings.Replace(s.URL, "http", "ws", 1)
	fmt.Printf("%v\n", wsURL)

	runtime.Goexit()
}
