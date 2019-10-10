package wsgrace

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// Grace wraps s.Handler to gracefully shutdown WebSocket connections.
// The returned function must be used to close the server instead of s.Close.
func Grace(s *http.Server) (closeFn func() error) {
	h := s.Handler
	var conns int64
	s.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&conns, 1)
		defer atomic.AddInt64(&conns, -1)

		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()

		r = r.WithContext(ctx)

		h.ServeHTTP(w, r)
	})

	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		err := s.Shutdown(ctx)
		if err != nil {
			return fmt.Errorf("server shutdown failed: %v", err)
		}

		t := time.NewTicker(time.Millisecond * 10)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if atomic.LoadInt64(&conns) == 0 {
					return nil
				}
			case <-ctx.Done():
				return fmt.Errorf("failed to wait for WebSocket connections: %v", ctx.Err())
			}
		}
	}
}
