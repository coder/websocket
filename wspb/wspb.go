package wspb

import (
	"io/ioutil"
	"time"

	"github.com/golang/protobuf/proto"
	"nhooyr.io/ws"
)

func Read(conn *ws.Conn, pb proto.Message) error {
	_, wsr, err := conn.ReadDataMessage()
	if err != nil {
		return err
	}

	deadline := time.Now().Add(time.Second * 15)
	err = wsr.SetDeadline(deadline)
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

func Write(conn *ws.Conn, pb proto.Message) error {
	buf, err := proto.Marshal(pb)
	if err != nil {
		return err
	}

	wsw := conn.WriteDataMessage(ws.OpText)

	deadline := time.Now().Add(time.Second * 15)
	err = wsw.SetDeadline(deadline)
	if err != nil {
		return err
	}

	_, err = wsw.Write(buf)
	if err != nil {
		return err
	}

	err = wsw.Close()
	return err
}
