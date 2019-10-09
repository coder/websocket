// +build !js

package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/wsecho"
)

func fork() net.Listener {
	if os.Getenv("FORKED") != "" {
		f := os.NewFile(3, "listener")
		l, err := net.FileListener(f)
		if err != nil {
			log.Fatalf("failed to create listener from fd: %+v", err)
		}
		return l
	}

	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		log.Fatalf("failed to listen: %+v", err)
	}
	f, err := l.(*net.TCPListener).File()
	if err != nil {
		log.Fatalf("failed to get file from tcp listener: %+v", err)
	}

	cmd := exec.Command(os.Args[0])
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("FORKED=true"),
	)
	cmd.ExtraFiles = append(cmd.ExtraFiles, f)
	err = cmd.Start()
	if err != nil {
		log.Fatalf("failed to start command: %+v", err)
	}

	fmt.Printf("ws://%v\n", l.Addr().String())
	os.Exit(0)

	panic("unreachable")
}

func main() {
	l := fork()

	err := serve(l)
	log.Fatalf("failed to serve: %+v", err)
}

func serve(l net.Listener) error {
	return http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))

}
