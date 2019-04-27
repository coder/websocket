package websocket

import (
	"context"
)

// Write writes p as a single data frame to the connection. This is an optimization
// method for when the entire message is in memory and does not need to be streamed
// to the peer via Writer.
//
// This prevents the allocation of the Writer.
// Furthermore Writer always has to write an additional fin frame when Close is
// called on the writer which can result in worse performance if the full message
// exceeds the buffer size which is 4096 right now as then an extra syscall
// will be necessary to complete the message.
func (c *Conn) Write(ctx context.Context, typ MessageType, p []byte) error {
	return c.writeSingleFrame(ctx, opcode(typ), p)
}
