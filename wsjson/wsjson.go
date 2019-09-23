// Package wsjson provides websocket helpers for JSON messages.
package wsjson // import "nhooyr.io/websocket/wsjson"

import (
	"context"
	"encoding/json"
	"fmt"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/bpool"
)

// Read reads a json message from c into v.
// It will reuse buffers to avoid allocations.
func Read(ctx context.Context, c *websocket.Conn, v interface{}) error {
	err := read(ctx, c, v)
	if err != nil {
		return fmt.Errorf("failed to read json: %w", err)
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
		return fmt.Errorf("unexpected frame type for json (expected %v): %v", websocket.MessageText, typ)
	}

	b := bpool.Get()
	defer bpool.Put(b)

	_, err = b.ReadFrom(r)
	if err != nil {
		return err
	}

	err = json.Unmarshal(b.Bytes(), v)
	if err != nil {
		c.Close(websocket.StatusInvalidFramePayloadData, "failed to unmarshal JSON")
		return fmt.Errorf("failed to unmarshal json: %w", err)
	}

	return nil
}

// Write writes the json message v to c.
// It will reuse buffers to avoid allocations.
func Write(ctx context.Context, c *websocket.Conn, v interface{}) error {
	err := write(ctx, c, v)
	if err != nil {
		return fmt.Errorf("failed to write json: %w", err)
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
		return fmt.Errorf("failed to encode json: %w", err)
	}

	err = w.Close()
	if err != nil {
		return err
	}
	return nil
}
