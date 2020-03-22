package websocket

import (
	"context"
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
// It's required as net/http's Shutdown and Close methods do not keep track of WebSocket
// connections.
//
// Make sure to Close or Shutdown the *http.Server first as you don't want to accept
// any new connections while the existing websockets are being shut down.
type Grace struct {
	handlersMu sync.Mutex
	closing    bool
	handlers   map[context.Context]context.CancelFunc
}

// Handler returns a handler that wraps around h to record
// all WebSocket connections accepted.
//
// Use Close or Shutdown to gracefully close recorded connections.
// Make sure to Close or Shutdown the *http.Server first.
func (g *Grace) Handler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		r = r.WithContext(ctx)

		ok := g.add(w, ctx, cancel)
		if !ok {
			return
		}
		defer g.del(ctx)

		h.ServeHTTP(w, r)
	})
}

func (g *Grace) add(w http.ResponseWriter, ctx context.Context, cancel context.CancelFunc) bool {
	g.handlersMu.Lock()
	defer g.handlersMu.Unlock()

	if g.closing {
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return false
	}

	if g.handlers == nil {
		g.handlers = make(map[context.Context]context.CancelFunc)
	}
	g.handlers[ctx] = cancel

	return true
}

func (g *Grace) del(ctx context.Context) {
	g.handlersMu.Lock()
	defer g.handlersMu.Unlock()

	delete(g.handlers, ctx)
}

// Close prevents the acceptance of new connections with
// http.StatusServiceUnavailable and closes all accepted
// connections with StatusGoingAway.
//
// Make sure to Close or Shutdown the *http.Server first.
func (g *Grace) Close() error {
	g.handlersMu.Lock()
	for _, cancel := range g.handlers {
		cancel()
	}
	g.handlersMu.Unlock()

	// Wait for all goroutines to exit.
	g.Shutdown(context.Background())

	return nil
}

// Shutdown prevents the acceptance of new connections and waits until
// all connections close. If the context is cancelled before that, it
// calls Close to close all connections immediately.
//
// Make sure to Close or Shutdown the *http.Server first.
func (g *Grace) Shutdown(ctx context.Context) error {
	defer g.Close()

	// Same poll period used by net/http.
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		if g.zeroHandlers() {
			return nil
		}

		select {
		case <-t.C:
		case <-ctx.Done():
			return fmt.Errorf("failed to shutdown WebSockets: %w", ctx.Err())
		}
	}
}

func (g *Grace) zeroHandlers() bool {
	g.handlersMu.Lock()
	defer g.handlersMu.Unlock()
	return len(g.handlers) == 0
}
