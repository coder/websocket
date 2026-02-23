package websocket

import (
	"errors"
	"fmt"
	"io"
)

var (
	_ io.ReadWriteCloser = (*h2ServerStream)(nil)
	_ io.ReadWriteCloser = (*h2ClientStream)(nil)
)

// h2ServerStream is a minimal io.ReadWriteCloser for an HTTP/2 extended CONNECT
// tunnel. Read reads from the request body and Write writes to the response
// writer. To ensure data transmission, the stream is flushed on write.
type h2ServerStream struct {
	io.ReadCloser              // http.Request.Body
	io.Writer                  // http.ResponseWriter
	flush         func() error // http.ResponseWriter
}

func (s *h2ServerStream) Read(p []byte) (int, error) {
	return s.ReadCloser.Read(p)
}

func (s *h2ServerStream) Write(p []byte) (int, error) {
	n, err := s.Writer.Write(p)
	if err != nil {
		return n, err
	}
	err = s.Flush()
	return n, err
}

func (s *h2ServerStream) Flush() error {
	if err := s.flush(); err != nil {
		return fmt.Errorf("h2ServerStream: failed to flush: %w", err)
	}
	return nil
}

func (s *h2ServerStream) Close() error {
	// TODO(mafredri): Verify if the flush is necessary before closing the reader.
	err := s.Flush()
	return errors.Join(err, s.ReadCloser.Close())
}

// h2ClientStream is a minimal io.ReadWriteCloser for an HTTP/2 extended CONNECT
// tunnel. Read reads from the response body and Write writes to the PipeWriter
// that feeds the request body.
type h2ClientStream struct {
	io.ReadCloser  // http.Response.Body
	io.WriteCloser // http.Request.Body
}

func (s *h2ClientStream) Close() error {
	return errors.Join(s.ReadCloser.Close(), s.WriteCloser.Close())
}
