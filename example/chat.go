package main

import (
	"context"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

type chatServer struct {
	subscribersMu sync.RWMutex
	subscribers   map[chan []byte]struct{}
}

func (cs *chatServer) subscribeHandler(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Print(err)
		return
	}

	cs.subscribe(r.Context(), c)
}

func (cs *chatServer) publishHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	body := io.LimitReader(r.Body, 8192)
	msg, err := ioutil.ReadAll(body)
	if err != nil {
		return
	}

	cs.publish(msg)
}

func (cs *chatServer) publish(msg []byte) {
	cs.subscribersMu.RLock()
	defer cs.subscribersMu.RUnlock()

	for c := range cs.subscribers {
		select {
		case c <- msg:
		default:
		}
	}
}

func (cs *chatServer) addSubscriber(msgs chan []byte) {
	cs.subscribersMu.Lock()
	if cs.subscribers == nil {
		cs.subscribers = make(map[chan []byte]struct{})
	}
	cs.subscribers[msgs] = struct{}{}
	cs.subscribersMu.Unlock()
}

func (cs *chatServer) deleteSubscriber(msgs chan []byte) {
	cs.subscribersMu.Lock()
	delete(cs.subscribers, msgs)
	cs.subscribersMu.Unlock()
}

func (cs *chatServer) subscribe(ctx context.Context, c *websocket.Conn) error {
	ctx = c.CloseRead(ctx)

	msgs := make(chan []byte, 16)
	cs.addSubscriber(msgs)
	defer cs.deleteSubscriber(msgs)

	for {
		select {
		case msg := <-msgs:
			err := writeTimeout(ctx, time.Second*5, c, msg)
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func writeTimeout(ctx context.Context, timeout time.Duration, c *websocket.Conn, msg []byte) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return c.Write(ctx, websocket.MessageText, msg)
}
