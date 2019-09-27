// +build js

package websocket

import (
	"context"
	"fmt"
)

func (c *Conn) WaitCloseFrame(ctx context.Context) error {
	select {
	case <-c.receivedCloseFrame:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("failed to wait for close frame: %w", ctx.Err())
	}
}
