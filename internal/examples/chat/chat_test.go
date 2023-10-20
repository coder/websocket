package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"nhooyr.io/websocket"
)

func Test_chatServer(t *testing.T) {
	t.Parallel()

	// This is a simple echo test with a single client.
	// The client sends a message and ensures it receives
	// it on its WebSocket.
	t.Run("simple", func(t *testing.T) {
		t.Parallel()

		url, closeFn := setupTest(t)
		defer closeFn()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()

		cl, err := newClient(ctx, url)
		assertSuccess(t, err)
		defer cl.Close()

		expMsg := randString(512)
		err = cl.publish(ctx, expMsg)
		assertSuccess(t, err)

		msg, err := cl.nextMessage()
		assertSuccess(t, err)

		if expMsg != msg {
			t.Fatalf("expected %v but got %v", expMsg, msg)
		}
	})

	// This test is a complex concurrency test.
	// 10 clients are started that send 128 different
	// messages of max 128 bytes concurrently.
	//
	// The test verifies that every message is seen by ever client
	// and no errors occur anywhere.
	t.Run("concurrency", func(t *testing.T) {
		t.Parallel()

		const nmessages = 128
		const maxMessageSize = 128
		const nclients = 16

		url, closeFn := setupTest(t)
		defer closeFn()

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		var clients []*client
		var clientMsgs []map[string]struct{}
		for i := 0; i < nclients; i++ {
			cl, err := newClient(ctx, url)
			assertSuccess(t, err)
			defer cl.Close()

			clients = append(clients, cl)
			clientMsgs = append(clientMsgs, randMessages(nmessages, maxMessageSize))
		}

		allMessages := make(map[string]struct{})
		for _, msgs := range clientMsgs {
			for m := range msgs {
				allMessages[m] = struct{}{}
			}
		}

		var wg sync.WaitGroup
		for i, cl := range clients {
			i := i
			cl := cl

			wg.Add(1)
			go func() {
				defer wg.Done()
				err := cl.publishMsgs(ctx, clientMsgs[i])
				if err != nil {
					t.Errorf("client %d failed to publish all messages: %v", i, err)
				}
			}()

			wg.Add(1)
			go func() {
				defer wg.Done()
				err := testAllMessagesReceived(cl, nclients*nmessages, allMessages)
				if err != nil {
					t.Errorf("client %d failed to receive all messages: %v", i, err)
				}
			}()
		}

		wg.Wait()
	})
}

// setupTest sets up chatServer that can be used
// via the returned url.
//
// Defer closeFn to ensure everything is cleaned up at
// the end of the test.
//
// chatServer logs will be logged via t.Logf.
func setupTest(t *testing.T) (url string, closeFn func()) {
	cs := newChatServer()
	cs.logf = t.Logf

	// To ensure tests run quickly under even -race.
	cs.subscriberMessageBuffer = 4096
	cs.publishLimiter.SetLimit(rate.Inf)

	s := httptest.NewServer(cs)
	return s.URL, func() {
		s.Close()
	}
}

// testAllMessagesReceived ensures that after n reads, all msgs in msgs
// have been read.
func testAllMessagesReceived(cl *client, n int, msgs map[string]struct{}) error {
	msgs = cloneMessages(msgs)

	for i := 0; i < n; i++ {
		msg, err := cl.nextMessage()
		if err != nil {
			return err
		}
		delete(msgs, msg)
	}

	if len(msgs) != 0 {
		return fmt.Errorf("did not receive all expected messages: %q", msgs)
	}
	return nil
}

func cloneMessages(msgs map[string]struct{}) map[string]struct{} {
	msgs2 := make(map[string]struct{}, len(msgs))
	for m := range msgs {
		msgs2[m] = struct{}{}
	}
	return msgs2
}

func randMessages(n, maxMessageLength int) map[string]struct{} {
	msgs := make(map[string]struct{})
	for i := 0; i < n; i++ {
		m := randString(randInt(maxMessageLength))
		if _, ok := msgs[m]; ok {
			i--
			continue
		}
		msgs[m] = struct{}{}
	}
	return msgs
}

func assertSuccess(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type client struct {
	url string
	c   *websocket.Conn
}

func newClient(ctx context.Context, url string) (*client, error) {
	c, _, err := websocket.Dial(ctx, url+"/subscribe", nil)
	if err != nil {
		return nil, err
	}

	cl := &client{
		url: url,
		c:   c,
	}

	return cl, nil
}

func (cl *client) publish(ctx context.Context, msg string) (err error) {
	defer func() {
		if err != nil {
			cl.c.Close(websocket.StatusInternalError, "publish failed")
		}
	}()

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

func (cl *client) publishMsgs(ctx context.Context, msgs map[string]struct{}) error {
	for m := range msgs {
		err := cl.publish(ctx, m)
		if err != nil {
			return err
		}
	}
	return nil
}

func (cl *client) nextMessage() (string, error) {
	typ, b, err := cl.c.Read(context.Background())
	if err != nil {
		return "", err
	}

	if typ != websocket.MessageText {
		cl.c.Close(websocket.StatusUnsupportedData, "expected text message")
		return "", fmt.Errorf("expected text message but got %v", typ)
	}
	return string(b), nil
}

func (cl *client) Close() error {
	return cl.c.Close(websocket.StatusNormalClosure, "")
}

// randString generates a random string with length n.
func randString(n int) string {
	b := make([]byte, n)
	_, err := rand.Reader.Read(b)
	if err != nil {
		panic(fmt.Sprintf("failed to generate rand bytes: %v", err))
	}

	s := strings.ToValidUTF8(string(b), "_")
	s = strings.ReplaceAll(s, "\x00", "_")
	if len(s) > n {
		return s[:n]
	}
	if len(s) < n {
		// Pad with =
		extra := n - len(s)
		return s + strings.Repeat("=", extra)
	}
	return s
}

// randInt returns a randomly generated integer between [0, max).
func randInt(max int) int {
	x, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		panic(fmt.Sprintf("failed to get random int: %v", err))
	}
	return int(x.Int64())
}
