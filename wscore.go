package ws

import (
	"nhooyr.io/ws/wscore"
)

type Opcode = wscore.Opcode

const (
	Continuation Opcode = iota
	Text
	Binary
	// 3 - 7 are reserved for further non-control frames.
	Close = 8
	Ping
	Pong
	// 11-16 are reserved for further control frames.
)

type StatusCode uint16

const (
	NormalClosure StatusCode = 1000 + iota
	GoingAway
	ProtocolError
	UnsupportedData
	// ...
)
