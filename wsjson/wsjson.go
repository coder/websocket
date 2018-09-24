package wsjson

import (
	"encoding/json"
	"time"

	"nhooyr.io/ws"
)

func Read(conn *ws.Conn, v interface{}) error {
	_, wsr, err := conn.ReadDataMessage()
	if err != nil {
		return err
	}

	deadline := time.Now().Add(time.Second * 15)
	err = wsr.SetDeadline(deadline)
	if err != nil {
		return err
	}

	d := json.NewDecoder()
	err = d.Decode(v)
	return err
}

func Write(conn *ws.Conn, v interface{}) error {
	wsw := conn.WriteDataMessage(ws.OpText)

	deadline := time.Now().Add(time.Second * 15)
	err := wsw.SetDeadline(deadline)
	if err != nil {
		return err
	}

	e := json.NewEncoder(wsw)
	err = e.Encode(v)
	if err != nil {
		return err
	}

	err = wsw.Close()
	return err
}
