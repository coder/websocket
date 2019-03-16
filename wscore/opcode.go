package wscore

// Opcode represents a WebSocket Opcode.
//go:generate stringer -type=Opcode
type Opcode int

const (
	OpContinuation Opcode = iota
	OpText
	OpBinary
	// 3 - 7 are reserved for further non-control frames.
	OpClose Opcode = 8 + iota
	OpPing
	OpPong
	// 11-16 are reserved for further control frames.
)
