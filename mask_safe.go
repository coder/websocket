// +build appengine

package ws

func realMask(key [4]byte, pos int, b []byte) int {
	return maskByByte(key, pos, b)
}
