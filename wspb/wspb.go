package wspb

import (
	"io/ioutil"

	"github.com/golang/protobuf/proto"
	"nhooyr.io/ws"
)

func ReadMessage(conn *ws.Conn, pb proto.Message) error {
	_, wsr, err := conn.ReadMessage()
	if err != nil {
		return err
	}

	buf, err := ioutil.ReadAll(wsr)
	if err != nil {
		return err
	}

	err = proto.Unmarshal(buf, pb)
	return err
}

func WriteMessage(conn *ws.Conn, pb proto.Message) error {
	buf, err := proto.Marshal(pb)
	if err != nil {
		return err
	}

	wsw := conn.MessageWriter(ws.OpText)
	_, err = wsw.Write(buf)
	if err != nil {
		return err
	}

	err = wsw.Close()
	return err
}
