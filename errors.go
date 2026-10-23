package websocket

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// ErrMessageTooBig is returned when a message exceeds the read limit.
var ErrMessageTooBig = errors.New("websocket: message too big")

// ErrCloseReasonTruncated is returned by Close when the reason did not fit in
// a close frame and a truncated reason was sent instead.
var ErrCloseReasonTruncated = errors.New("websocket: close reason truncated")

// maxCloseReason is the 125 byte control frame payload limit minus the 2 byte
// status code, see RFC 6455 section 5.5.
const maxCloseReason = 123

// truncateCloseReason returns the longest prefix of reason that fits in a
// close frame without splitting a valid UTF-8 encoded rune. The error wraps
// ErrCloseReasonTruncated if reason was cut.
func truncateCloseReason(reason string) (string, error) {
	if len(reason) <= maxCloseReason {
		return reason, nil
	}
	n := maxCloseReason
	// Back off only to keep a valid multi-byte rune whole. Invalid UTF-8 is
	// cut as is because the library does not validate it.
	for i := n; i > n-utf8.UTFMax; i-- {
		if utf8.RuneStart(reason[i]) {
			r, size := utf8.DecodeRuneInString(reason[i:])
			if (r != utf8.RuneError || size > 1) && i+size > n {
				n = i
			}
			break
		}
	}
	return reason[:n], fmt.Errorf("%w: sent %d of %d bytes", ErrCloseReasonTruncated, n, len(reason))
}
