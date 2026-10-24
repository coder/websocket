package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
)

// broadcastServer broadcasts every message published to /publish to all
// WebSocket clients subscribed to /subscribe.
//
// Each message is wrapped in a single websocket.PreparedMessage, so it is
// compressed once per broadcast instead of once per subscriber.
type broadcastServer struct {
	// subscriberMessageBuffer controls the max number
	// of messages that can be queued for a subscriber
	// before it is kicked.
	//
	// Defaults to 16.
	subscriberMessageBuffer int

	// logf controls where logs are sent.
	logf func(f string, v ...any)

	// serveMux routes the various endpoints to the appropriate handler.
	serveMux http.ServeMux

	subscribersMu sync.Mutex
	subscribers   map[*subscriber]struct{}
}

// newBroadcastServer constructs a broadcastServer with the defaults.
func newBroadcastServer(logf func(f string, v ...any)) *broadcastServer {
	bs := &broadcastServer{
		subscriberMessageBuffer: 16,
		logf:                    logf,
		subscribers:             make(map[*subscriber]struct{}),
	}
	bs.serveMux.HandleFunc("/subscribe", bs.subscribeHandler)
	bs.serveMux.HandleFunc("/publish", bs.publishHandler)

	return bs
}

// subscriber represents a subscriber.
// Messages are sent on the msgs channel and if the client
// cannot keep up with the messages, the slow channel is closed.
type subscriber struct {
	msgs     chan *websocket.PreparedMessage
	slow     chan struct{}
	slowOnce sync.Once
}

func (s *subscriber) closeSlow() {
	s.slowOnce.Do(func() {
		close(s.slow)
	})
}

func (bs *broadcastServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bs.serveMux.ServeHTTP(w, r)
}

// subscribeHandler accepts the WebSocket connection and then subscribes
// it to all future messages.
func (bs *broadcastServer) subscribeHandler(w http.ResponseWriter, r *http.Request) {
	err := bs.subscribe(w, r)
	if errors.Is(err, context.Canceled) {
		return
	}
	if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
		websocket.CloseStatus(err) == websocket.StatusGoingAway {
		return
	}
	if err != nil {
		bs.logf("%v", err)
		return
	}
}

// publishHandler reads the request body with a limit of 65536 bytes and then
// publishes the received message.
//
// Messages are sent as text, which must be valid UTF-8, so other bodies are
// rejected instead of being sent as invalid text messages.
func (bs *broadcastServer) publishHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	body := http.MaxBytesReader(w, r.Body, 65536)
	msg, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
		return
	}
	if !utf8.Valid(msg) {
		http.Error(w, "message must be valid UTF-8", http.StatusBadRequest)
		return
	}

	bs.publish(websocket.NewPreparedMessage(websocket.MessageText, msg))

	w.WriteHeader(http.StatusAccepted)
}

// subscribe subscribes the given WebSocket to all broadcast messages.
// The subscriber is registered before the WebSocket is accepted, so it
// receives every message published after the client's Dial returns.
//
// It uses CloseRead to keep reading from the connection to process control
// messages and cancel the context if the connection drops.
func (bs *broadcastServer) subscribe(w http.ResponseWriter, r *http.Request) error {
	s := &subscriber{
		msgs: make(chan *websocket.PreparedMessage, bs.subscriberMessageBuffer),
		slow: make(chan struct{}),
	}
	bs.addSubscriber(s)
	defer bs.deleteSubscriber(s)

	// Without context takeover, connections compress messages independently
	// of each other, which allows sharing the compressed payload.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionNoContextTakeover,
	})
	if err != nil {
		return err
	}
	defer c.CloseNow()

	ctx := c.CloseRead(context.Background())

	for {
		select {
		case pm := <-s.msgs:
			err := writeTimeout(ctx, time.Second*5, c, pm)
			if err != nil {
				return err
			}
		case <-s.slow:
			return c.Close(websocket.StatusPolicyViolation, "connection too slow to keep up with messages")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// publish publishes the pm to all subscribers.
// It never blocks and so messages to slow subscribers
// are dropped.
func (bs *broadcastServer) publish(pm *websocket.PreparedMessage) {
	bs.subscribersMu.Lock()
	defer bs.subscribersMu.Unlock()

	for s := range bs.subscribers {
		select {
		case s.msgs <- pm:
		default:
			s.closeSlow()
		}
	}
}

// addSubscriber registers a subscriber.
func (bs *broadcastServer) addSubscriber(s *subscriber) {
	bs.subscribersMu.Lock()
	bs.subscribers[s] = struct{}{}
	bs.subscribersMu.Unlock()
}

// deleteSubscriber deletes the given subscriber.
func (bs *broadcastServer) deleteSubscriber(s *subscriber) {
	bs.subscribersMu.Lock()
	delete(bs.subscribers, s)
	bs.subscribersMu.Unlock()
}

func writeTimeout(ctx context.Context, timeout time.Duration, c *websocket.Conn, pm *websocket.PreparedMessage) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return c.WritePrepared(ctx, pm)
}
