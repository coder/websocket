package util

// WriterFunc is used to implement one off io.Writers.
type WriterFunc func(p []byte) (int, error)

func (f WriterFunc) Write(p []byte) (int, error) {
	return f(p)
}
