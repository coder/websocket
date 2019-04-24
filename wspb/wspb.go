// Package wspb provides helpers for protobuf messages.
package wspb

import (
	"context"
	"io"
	"io/ioutil"

	"github.com/golang/protobuf/proto"
	"golang.org/x/xerrors"

	"nhooyr.io/websocket"
)

// Read reads a protobuf message from c into v.
// It will read a message up to 32768 bytes in length.
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
		return xerrors.Errorf("unexpected frame type for protobuf (expected %v): %v", websocket.MessageBinary, typ)
	}

	r = io.LimitReader(r, 32768)

	b, err := ioutil.ReadAll(r)
	if err != nil {
		return xerrors.Errorf("failed to read message: %w", err)
	}

	err = proto.Unmarshal(b, v)
	if err != nil {
		return xerrors.Errorf("failed to unmarshal protobuf: %w", err)
	}

	return nil
}

// Write writes the protobuf message v to c.
func Write(ctx context.Context, c *websocket.Conn, v proto.Message) error {
	err := write(ctx, c, v)
	if err != nil {
		return xerrors.Errorf("failed to write protobuf: %w", err)
	}
	return nil
}

func write(ctx context.Context, c *websocket.Conn, v proto.Message) error {
	b, err := proto.Marshal(v)
	if err != nil {
		return xerrors.Errorf("failed to marshal protobuf: %w", err)
	}

	w, err := c.Writer(ctx, websocket.MessageBinary)
	if err != nil {
		return err
	}

	_, err = w.Write(b)
	if err != nil {
		return err
	}

	err = w.Close()
	if err != nil {
		return err
	}
	return nil
}
