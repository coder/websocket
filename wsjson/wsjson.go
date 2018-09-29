package wsjson

import (
	"encoding/json"
	"fmt"

	"nhooyr.io/ws"
)

func ReadMessage(conn *ws.Conn, v interface{}) error {
	op, wsr, err := conn.ReadMessage()
	if err != nil {
		return err
	}

	if op != ws.OpText {
		return fmt.Errorf("unexpected op type for json: %v", op)
	}

	d := json.NewDecoder(wsr)
	err = d.Decode(v)
	return err
}

func WriteMessage(conn *ws.Conn, v interface{}) error {
	wsw := conn.MessageWriter(ws.OpText)

	e := json.NewEncoder(wsw)
	err := e.Encode(v)
	if err != nil {
		return err
	}

	err = wsw.Close()
	return err
}
