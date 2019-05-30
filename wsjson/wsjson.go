// Package wsjson provides helpers for JSON messages.
package wsjson

import (
	"context"
	"encoding/json"
	"io"

	"golang.org/x/xerrors"

	"nhooyr.io/websocket"
)

// Read reads a json message from c into v.
// For security reasons, it will not read messages
// larger than 32768 bytes.
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

	r = io.LimitReader(r, 32768)

	d := json.NewDecoder(r)
	err = d.Decode(v)
	if err != nil {
		return xerrors.Errorf("failed to decode json: %w", err)
	}

	// Have to ensure we read till EOF.
	// Unfortunate but necessary evil for now. Can improve later.
	// The code to do this automatically gets complicated fast because
	// we support concurrent reading.
	// So the Reader has to synchronize with Read somehow.
	// Maybe its best to bring back the old readLoop?
	_, err = r.Read([]byte{0})
	if !xerrors.Is(err, io.EOF) {
		return xerrors.Errorf("more data than needed in reader")
	}

	return nil
}

// Write writes the json message v to c.
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
