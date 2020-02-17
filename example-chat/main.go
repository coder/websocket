package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
)

func main() {
	err := run()
	if err != nil {
		log.Fatal(err)
	}
}

func run() error {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return err
	}
	fmt.Printf("listening on http://%v\n", l.Addr())

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
