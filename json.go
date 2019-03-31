package websocket

import (
	"context"
	"encoding/json"

	"golang.org/x/xerrors"
)

// JSONConn wraps around a Conn with JSON helpers.
type JSONConn struct {
	Conn *Conn
}

// Read reads a json message into v.
func (jc JSONConn) Read(ctx context.Context, v interface{}) error {
	err := jc.read(ctx, v)
	if err != nil {
		return xerrors.Errorf("failed to read json: %w", err)
	}
	return nil
}

func (jc *JSONConn) read(ctx context.Context, v interface{}) error {
	typ, r, err := jc.Conn.ReadMessage(ctx)
	if err != nil {
		return err
	}

	if typ != DataText {
		return xerrors.Errorf("unexpected frame type for json (expected DataText): %v", typ)
	}

	r.Limit(131072)
	r.SetContext(ctx)

	d := json.NewDecoder(r)
	err = d.Decode(v)
	if err != nil {
		return xerrors.Errorf("failed to decode json: %w", err)
	}
	return nil
}

// Write writes the json message v.
func (jc JSONConn) Write(ctx context.Context, v interface{}) error {
	err := jc.write(ctx, v)
	if err != nil {
		return xerrors.Errorf("failed to write json: %w", err)
	}
	return nil
}

func (jc JSONConn) write(ctx context.Context, v interface{}) error {
	w := jc.Conn.MessageWriter(DataText)
	w.SetContext(ctx)

	e := json.NewEncoder(w)
	err := e.Encode(v)
	if err != nil {
		return xerrors.Errorf("failed to encode json: %w", err)
	}

	err = w.Close()
	if err != nil {
		return err
	}
	return nil
}
