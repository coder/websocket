package websocket

import (
	"context"
)

// Write writes p as a single data frame to the connection. This is an optimization
// method for when the entire message is in memory and does not need to be streamed
// to the peer via Writer.
//
// Both paths are zero allocation but Writer always has
// to write an additional fin frame when Close is called on the writer which
// can result in worse performance if the full message exceeds the buffer size
// which is 4096 right now as then two syscalls will be necessary to complete the message.
// TODO this is no good as we cannot write daata frame msg in between other ones
func (c *Conn) Write(ctx context.Context, typ MessageType, p []byte) error {
	return c.writeControl(ctx, opcode(typ), p)
}
