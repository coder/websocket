package websocket

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Grace enables graceful shutdown of accepted WebSocket connections.
//
// Use Handler to wrap WebSocket handlers to record accepted connections
// and then use Close or Shutdown to gracefully close these connections.
//
// Grace is intended to be used in harmony with net/http.Server's Shutdown and Close methods.
type Grace struct {
	mu      sync.Mutex
	closing bool
	conns   map[*Conn]struct{}
}

// Handler returns a handler that wraps around h to record
// all WebSocket connections accepted.
//
// Use Close or Shutdown to gracefully close recorded connections.
func (g *Grace) Handler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), gracefulContextKey{}, g)
		r = r.WithContext(ctx)
		h.ServeHTTP(w, r)
	})
}

func (g *Grace) isClosing() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closing
}

func graceFromRequest(r *http.Request) *Grace {
	g, _ := r.Context().Value(gracefulContextKey{}).(*Grace)
	return g
}

func (g *Grace) addConn(c *Conn) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		c.Close(StatusGoingAway, "server shutting down")
		return errors.New("server shutting down")
	}
	if g.conns == nil {
		g.conns = make(map[*Conn]struct{})
	}
	g.conns[c] = struct{}{}
	c.g = g
	return nil
}

func (g *Grace) delConn(c *Conn) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.conns, c)
}

type gracefulContextKey struct{}

// Close prevents the acceptance of new connections with
// http.StatusServiceUnavailable and closes all accepted
// connections with StatusGoingAway.
func (g *Grace) Close() error {
	g.mu.Lock()
	g.closing = true
	var wg sync.WaitGroup
	for c := range g.conns {
		wg.Add(1)
		go func(c *Conn) {
			defer wg.Done()
			c.Close(StatusGoingAway, "server shutting down")
		}(c)

		delete(g.conns, c)
	}
	g.mu.Unlock()

	wg.Wait()

	return nil
}

// Shutdown prevents the acceptance of new connections and waits until
// all connections close. If the context is cancelled before that, it
// calls Close to close all connections immediately.
func (g *Grace) Shutdown(ctx context.Context) error {
	defer g.Close()

	g.mu.Lock()
	g.closing = true
	g.mu.Unlock()

	// Same poll period used by net/http.
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		if g.zeroConns() {
			return nil
		}

		select {
		case <-t.C:
		case <-ctx.Done():
			return fmt.Errorf("failed to shutdown WebSockets: %w", ctx.Err())
		}
	}
}

func (g *Grace) zeroConns() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.conns) == 0
}
