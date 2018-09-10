package ws

func Mask() {
	Mask()
}

func maskByByte(key [4]byte, pos int, b []byte) int {
	for i := range b {
		b[i] ^= key[pos&3]
		pos++
	}

	return pos
}
