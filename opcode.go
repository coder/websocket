package websocket

// opcode represents a WebSocket Opcode.
//go:generate stringer -type=opcode
type opcode int

// opcode constants.
const (
	opContinuation opcode = iota
	opText
	opBinary
	// 3 - 7 are reserved for further non-control frames.
	opClose opcode = 8 + iota
	opPing
	opPong
	// 11-16 are reserved for further control frames.
)
