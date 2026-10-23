package websocket

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// maxControlPayload is the maximum length of a control frame payload.
// See https://tools.ietf.org/html/rfc6455#section-5.5.
const maxControlPayload = 125

// Reserve 2 payload bytes for the status code.
const maxCloseReason = maxControlPayload - 2

func wireCloseReason(reason string) (string, error) {
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
