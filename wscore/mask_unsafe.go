// The code in this file was originally taken from github.com/websocket/gorilla

// +build !appengine

package wscore

import "unsafe"

const wordSize = unsafe.Sizeof(uintptr(0))

func mask(key [4]byte, pos int, p []byte) int {
	if len(p) < 2*int(wordSize) {
		// Mask one byte at a time for small buffers.
		return maskByByte(key, pos, p)
	}

	arrayAddr := uintptr(unsafe.Pointer(&p[0]))
	offset := arrayAddr % wordSize
	if offset > 0 {
		// Mask one byte at a time to word boundary.
		left := wordSize - offset
		pos = maskByByte(key, pos, p[:left])
		p = p[left:]
	}

	// Create aligned word size key.
	var k [wordSize]byte
	for i := range k {
		k[i] = key[(pos+i)&3]
	}
	kw := *(*uintptr)(unsafe.Pointer(&k))

	// Truncate the number of bits after word boundary from len(b).
	n := len(p) / int(wordSize) * int(wordSize)

	arrayAddr = uintptr(unsafe.Pointer(&p[0]))
	// Mask one word at a time.
	for i := 0; i < n; i += int(wordSize) {
		arrayAddr += uintptr(i)

		wordAddr := (*uintptr)(unsafe.Pointer(arrayAddr))
		*wordAddr ^= kw
	}

	// No need to adjust pos because it is guaranteed to stay the same.

	// Mask one byte at a time for remaining bytes.
	p = p[n:]
	return maskByByte(key, pos, p)
}
