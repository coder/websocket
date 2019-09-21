package websocket

// MessageType represents the type of a WebSocket message.
// See https://tools.ietf.org/html/rfc6455#section-5.6
type MessageType int

//go:generate go run golang.org/x/tools/cmd/stringer -type=MessageType

// MessageType constants.
const (
	// MessageText is for UTF-8 encoded text messages like JSON.
	MessageText MessageType = iota + 1
	// MessageBinary is for binary messages like Protobufs.
	MessageBinary
)

// Above I've explicitly included the types of the constants for stringer.
