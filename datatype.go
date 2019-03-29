package ws

import (
	"nhooyr.io/ws/wscore"
)

// DataType represents the Opcode of a WebSocket data frame.
//go:generate stringer -type=DataType
type DataType int

// DataType constants.
const (
	Text   DataType = DataType(wscore.OpText)
	Binary DataType = DataType(wscore.OpBinary)
)
