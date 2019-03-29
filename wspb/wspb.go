package wspb

import (
	"context"

	"github.com/golang/protobuf/proto"

	"nhooyr.io/ws"
)

// Read reads a protobuf message from c into v.
func Read(ctx context.Context, c *ws.Conn, v proto.Message) error {
	panic("TODO")
}

// Write writes the protobuf message into c.
func Write(ctx context.Context, c *ws.Conn, v proto.Message) error {
	panic("TODO")
}
