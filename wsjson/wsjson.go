// Package wsjson provides websocket helpers for JSON messages.
package wsjson

import (
	"context"
	"encoding/json"

	"golang.org/x/xerrors"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/bpool"
)

// Read reads a json message from c into v.
// If the message is larger than 128 bytes, it will use a buffer
// from a pool instead of performing an allocation.
func Read(ctx context.Context, c *websocket.Conn, v interface{}) error {
	err := read(ctx, c, v)
	if err != nil {
		return xerrors.Errorf("failed to read json: %w", err)
	}
	return nil
}

func read(ctx context.Context, c *websocket.Conn, v interface{}) error {
	typ, r, err := c.Reader(ctx)
	if err != nil {
		return err
	}

	if typ != websocket.MessageText {
		c.Close(websocket.StatusUnsupportedData, "can only accept text messages")
		return xerrors.Errorf("unexpected frame type for json (expected %v): %v", websocket.MessageText, typ)
	}

	b := bpool.Get()
	defer func() {
		bpool.Put(b)
	}()

	_, err = b.ReadFrom(r)
	if err != nil {
		return err
	}

	err = json.Unmarshal(b.Bytes(), v)
	if err != nil {
		return xerrors.Errorf("failed to unmarshal json: %w", err)
	}

	return nil
}

// Write writes the json message v to c.
// It uses json.Encoder which automatically reuses buffers.
func Write(ctx context.Context, c *websocket.Conn, v interface{}) error {
	err := write(ctx, c, v)
	if err != nil {
		return xerrors.Errorf("failed to write json: %w", err)
	}
	return nil
}

func write(ctx context.Context, c *websocket.Conn, v interface{}) error {
	w, err := c.Writer(ctx, websocket.MessageText)
	if err != nil {
		return err
	}

	// We use Encode because it automatically enables buffer reuse without us
	// needing to do anything. Though see https://github.com/golang/go/issues/27735
	e := json.NewEncoder(w)
	err = e.Encode(v)
	if err != nil {
		return xerrors.Errorf("failed to encode json: %w", err)
	}

	err = w.Close()
	if err != nil {
		return err
	}
	return nil
}
