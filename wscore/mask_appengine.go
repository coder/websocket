// +build appengine

package wscore

func mask(key [4]byte, pos int, b []byte) int {
	return maskByByte(key, pos, b)
}
