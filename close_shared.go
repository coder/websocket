package websocket

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// maxControlPayload is the maximum length of a control frame payload.
// See https://tools.ietf.org/html/rfc6455#section-5.5.
const maxControlPayload = 125

// maxCloseReason is maxControlPayload minus the 2-byte status code.
const maxCloseReason = maxControlPayload - 2

// wireCloseReason returns the reason to send, also when the error is non-nil.
// The error reports a truncated reason.
func wireCloseReason(reason string) (string, error) {
	// RFC 6455 section 5.5.1 requires a UTF-8 reason.
	valid := strings.ToValidUTF8(reason, "\uFFFD")
	if len(valid) <= maxCloseReason {
		return valid, nil
	}
	n := maxCloseReason
	for !utf8.RuneStart(valid[n]) {
		n--
	}
	var replaced string
	if len(valid) != len(reason) {
		replaced = " after replacing invalid UTF-8"
	}
	return valid[:n], fmt.Errorf("%w from %d to %d bytes%s", ErrCloseReasonTruncated, len(valid), n, replaced)
}
