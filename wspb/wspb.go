// Package wspb provides websocket helpers for protobuf messages.
package wspb // import "nhooyr.io/websocket/wspb"

import (
	"bytes"
	"context"
	"sync"

	"github.com/golang/protobuf/proto"
	"golang.org/x/xerrors"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/bpool"
)

// Read reads a protobuf message from c into v.
// It will reuse buffers to avoid allocations.
func Read(ctx context.Context, c *websocket.Conn, v proto.Message) error {
	err := read(ctx, c, v)
	if err != nil {
		return xerrors.Errorf("failed to read protobuf: %w", err)
	}
	return nil
}

func read(ctx context.Context, c *websocket.Conn, v proto.Message) error {
	typ, r, err := c.Reader(ctx)
	if err != nil {
		return err
	}

	if typ != websocket.MessageBinary {
		c.Close(websocket.StatusUnsupportedData, "can only accept binary messages")
		return xerrors.Errorf("unexpected frame type for protobuf (expected %v): %v", websocket.MessageBinary, typ)
	}

	b := bpool.Get()
	defer func() {
		bpool.Put(b)
	}()

	_, err = b.ReadFrom(r)
	if err != nil {
		return err
	}

	err = proto.Unmarshal(b.Bytes(), v)
	if err != nil {
		c.Close(websocket.StatusInvalidFramePayloadData, "failed to unmarshal protobuf")
		return xerrors.Errorf("failed to unmarshal protobuf: %w", err)
	}

	return nil
}

// Write writes the protobuf message v to c.
// It will reuse buffers to avoid allocations.
func Write(ctx context.Context, c *websocket.Conn, v proto.Message) error {
	err := write(ctx, c, v)
	if err != nil {
		return xerrors.Errorf("failed to write protobuf: %w", err)
	}
	return nil
}

var writeBufPool sync.Pool

func write(ctx context.Context, c *websocket.Conn, v proto.Message) error {
	b := bpool.Get()
	pb := proto.NewBuffer(b.Bytes())
	defer func() {
		bpool.Put(bytes.NewBuffer(pb.Bytes()))
	}()

	err := pb.Marshal(v)
	if err != nil {
		return xerrors.Errorf("failed to marshal protobuf: %w", err)
	}

	return c.Write(ctx, websocket.MessageBinary, pb.Bytes())
}
