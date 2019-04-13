package websocket

// DataType represents the Opcode of a WebSocket data frame.
type DataType int

//go:generate go run golang.org/x/tools/cmd/stringer -type=DataType

// DataType constants.
const (
	DataText   DataType = DataType(opText)
	DataBinary DataType = DataType(opBinary)
)
