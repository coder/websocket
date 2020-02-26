// +build !js

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func TestGrace(t *testing.T) {
	t.Parallel()

	var cs chatServer
	var g websocket.Grace
	s := httptest.NewServer(g.Handler(&cs))
	defer s.Close()
	defer g.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	cl1, err := newClient(ctx, s.URL)
	assertSuccess(t, err)
	defer cl1.Close()

	cl2, err := newClient(ctx, s.URL)
	assertSuccess(t, err)
	defer cl2.Close()

	err = cl1.publish(ctx, "hello")
	assertSuccess(t, err)

	assertReceivedMessage(ctx, cl1, "hello")
	assertReceivedMessage(ctx, cl2, "hello")
}

type client struct {
	msgs chan string
	url  string
	c    *websocket.Conn
}

func newClient(ctx context.Context, url string) (*client, error) {
	wsURL := strings.ReplaceAll(url, "http://", "ws://")
	c, _, err := websocket.Dial(ctx, wsURL+"/subscribe", nil)
	if err != nil {
		return nil, err
	}

	cl := &client{
		msgs: make(chan string, 16),
		url:  url,
		c:    c,
	}
	go cl.readLoop()

	return cl, nil
}

func (cl *client) readLoop() {
	defer cl.c.Close(websocket.StatusInternalError, "")
	defer close(cl.msgs)

	for {
		typ, b, err := cl.c.Read(context.Background())
		if err != nil {
			return
		}

		if typ != websocket.MessageText {
			cl.c.Close(websocket.StatusUnsupportedData, "expected text message")
			return
		}

		select {
		case cl.msgs <- string(b):
		default:
			cl.c.Close(websocket.StatusInternalError, "messages coming in too fast to handle")
			return
		}
	}
}

func (cl *client) receive(ctx context.Context) (string, error) {
	select {
	case msg, ok := <-cl.msgs:
		if !ok {
			return "", errors.New("client closed")
		}
		return msg, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (cl *client) publish(ctx context.Context, msg string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, cl.url+"/publish", strings.NewReader(msg))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("publish request failed: %v", resp.StatusCode)
	}
	return nil
}

func (cl *client) Close() error {
	return cl.c.Close(websocket.StatusNormalClosure, "")
}

func assertSuccess(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func assertReceivedMessage(ctx context.Context, cl *client, msg string) error {
	msg, err := cl.receive(ctx)
	if err != nil {
		return err
	}
	if msg != "hello" {
		return fmt.Errorf("expected hello but got %q", msg)
	}
	return nil
}
