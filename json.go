package websocket

import (
	"context"
)

// ReadJSON reads a json message from c into v.
func ReadJSON(ctx context.Context, c *Conn, v interface{}) error {
	panic("TODO")
}

// WriteJSON writes the json message v into c.
func WriteJSON(ctx context.Context, c *Conn, v interface{}) error {
	panic("TODO")
}
