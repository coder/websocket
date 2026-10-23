package websocket

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// maxCloseReason is the 125 byte control frame payload limit minus the 2 byte
// status code, see RFC 6455 section 5.5.
const maxCloseReason = 123

// wireCloseReason returns reason as Close sends it: invalid UTF-8 replaced
// with U+FFFD, then cut to the longest prefix of at most maxCloseReason bytes
// that does not split a rune. The error wraps ErrCloseReasonTruncated if the
// reason was cut.
func wireCloseReason(reason string) (string, error) {
	// RFC 6455 section 5.5.1 defines the reason as UTF-8, and peers that
	// check it fail the connection (section 8.1) instead of reading the code.
	valid := strings.ToValidUTF8(reason, "\uFFFD")
	if len(valid) <= maxCloseReason {
		return valid, nil
	}
	n := maxCloseReason
	for !utf8.RuneStart(valid[n]) {
		n--
	}
	return valid[:n], fmt.Errorf("%w from %d to %d bytes", ErrCloseReasonTruncated, len(valid), n)
}
