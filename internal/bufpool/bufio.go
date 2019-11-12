package bufpool

import (
	"bufio"
	"io"
	"sync"
)

var readerPool = sync.Pool{
	New: func() interface{} {
		return bufio.NewReader(nil)
	},
}

func GetReader(r io.Reader) *bufio.Reader {
	br := readerPool.Get().(*bufio.Reader)
	br.Reset(r)
	return br
}

func PutReader(br *bufio.Reader) {
	readerPool.Put(br)
}

var writerPool = sync.Pool{
	New: func() interface{} {
		return bufio.NewWriter(nil)
	},
}

func GetWriter(w io.Writer) *bufio.Writer {
	bw := writerPool.Get().(*bufio.Writer)
	bw.Reset(w)
	return bw
}

func PutWriter(bw *bufio.Writer) {
	writerPool.Put(bw)
}

