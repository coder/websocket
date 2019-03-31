package websocket

// DataType represents the Opcode of a WebSocket data frame.
//go:generate go run golang.org/x/tools/cmd/stringer -type=DataType
type DataType int

// DataType constants.
const (
	Text   DataType = DataType(opText)
	Binary DataType = DataType(opBinary)
)
