//go:build amd64 || arm64

package websocket

import "testing"

func TestMaskASM(t *testing.T) {
	t.Parallel()

	testMask(t, "maskASM", mask)
}
