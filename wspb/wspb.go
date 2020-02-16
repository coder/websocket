// Package wspb provides helpers for reading and writing protobuf messages.
package wspb // import "nhooyr.io/websocket/wspb"

import (
	"bytes"
	"context"
	"fmt"

	"github.com/golang/protobuf/proto"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/bpool"
	"nhooyr.io/websocket/internal/errd"
)

// Read reads a protobuf message from c into v.
// It will reuse buffers in between calls to avoid allocations.
func Read(ctx context.Context, c *websocket.Conn, v proto.Message) error {
	return read(ctx, c, v)
}

func read(ctx context.Context, c *websocket.Conn, v proto.Message) (err error) {
	defer errd.Wrap(&err, "failed to read protobuf message")

	typ, r, err := c.Reader(ctx)
	if err != nil {
		return err
	}

	if typ != websocket.MessageBinary {
		c.Close(websocket.StatusUnsupportedData, "expected binary message")
		return fmt.Errorf("expected binary message for protobuf but got: %v", typ)
	}

	b := bpool.Get()
	defer bpool.Put(b)

	_, err = b.ReadFrom(r)
	if err != nil {
		return err
	}

	err = proto.Unmarshal(b.Bytes(), v)
	if err != nil {
		c.Close(websocket.StatusInvalidFramePayloadData, "failed to unmarshal protobuf")
		return fmt.Errorf("failed to unmarshal protobuf: %w", err)
	}

	return nil
}

// Write writes the protobuf message v to c.
// It will reuse buffers in between calls to avoid allocations.
func Write(ctx context.Context, c *websocket.Conn, v proto.Message) error {
	return write(ctx, c, v)
}

func write(ctx context.Context, c *websocket.Conn, v proto.Message) (err error) {
	defer errd.Wrap(&err, "failed to write protobuf message")

	b := bpool.Get()
	pb := proto.NewBuffer(b.Bytes())
	defer func() {
		bpool.Put(bytes.NewBuffer(pb.Bytes()))
	}()

	err = pb.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal protobuf: %w", err)
	}

	return c.Write(ctx, websocket.MessageBinary, pb.Bytes())
}
