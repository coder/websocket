// +build js

package wsjson

import (
	"context"
	"encoding/json"
	"fmt"

	"nhooyr.io/websocket"
)

// Read reads a json message from c into v.
func Read(ctx context.Context, c *websocket.Conn, v interface{}) error {
	err := read(ctx, c, v)
	if err != nil {
		return fmt.Errorf("failed to read json: %w", err)
	}
	return nil
}

func read(ctx context.Context, c *websocket.Conn, v interface{}) error {
	typ, b, err := c.Read(ctx)
	if err != nil {
		return err
	}

	if typ != websocket.MessageText {
		c.Close(websocket.StatusUnsupportedData, "can only accept text messages")
		return fmt.Errorf("unexpected frame type for json (expected %v): %v", websocket.MessageText, typ)
	}

	err = json.Unmarshal(b, v)
	if err != nil {
		c.Close(websocket.StatusInvalidFramePayloadData, "failed to unmarshal JSON")
		return fmt.Errorf("failed to unmarshal json: %w", err)
	}

	return nil
}

// Write writes the json message v to c.
func Write(ctx context.Context, c *websocket.Conn, v interface{}) error {
	err := write(ctx, c, v)
	if err != nil {
		return fmt.Errorf("failed to write json: %w", err)
	}
	return nil
}

func write(ctx context.Context, c *websocket.Conn, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return c.Write(ctx, websocket.MessageBinary, b)
}
