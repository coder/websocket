package main

import (
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	log.SetFlags(0)

	err := run()
	if err != nil {
		log.Fatal(err)
	}
}

// run initializes the chatServer and routes and then
// starts a http.Server for the passed in address.
func run() error {
	if len(os.Args) < 2 {
		return errors.New("please provide an address to listen on as the first argument")
	}

	l, err := net.Listen("tcp", os.Args[1])
	if err != nil {
		return err
	}
	log.Printf("listening on http://%v", l.Addr())

	var ws chatServer

	m := http.NewServeMux()
	m.Handle("/", http.FileServer(http.Dir(".")))
	m.HandleFunc("/subscribe", ws.subscribeHandler)
	m.HandleFunc("/publish", ws.publishHandler)

	s := http.Server{
		Handler:      m,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}
	return s.Serve(l)
}
