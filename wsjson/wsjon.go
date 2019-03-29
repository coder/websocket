package wsjson

import (
	"context"

	"nhooyr.io/ws"
)

// Read reads a json message from c into v.
func Read(ctx context.Context, c *ws.Conn, v interface{}) error {
	panic("TODO")
}

// Write writes the json message v into c.
func Write(ctx context.Context, c *ws.Conn, v interface{}) error {
	panic("TODO")
}
