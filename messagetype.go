package websocket

// MessageType represents the Opcode of a WebSocket data frame.
type MessageType int

//go:generate go run golang.org/x/tools/cmd/stringer -type=MessageType

// MessageType constants.
const (
	MessageText   MessageType = MessageType(opText)
	MessageBinary MessageType = MessageType(opBinary)
)
