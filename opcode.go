package websocket

// opcode represents a WebSocket Opcode.
//go:generate go run golang.org/x/tools/cmd/stringer -type=opcode
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

func (o opcode) controlOp() bool {
	switch o {
	case opClose, opPing, opPong:
		return true
	}
	return false
}
