package wstest

import (
	"net/http/httptest"
	"strings"
)

// URL returns the ws url for s.
func URL(s *httptest.Server) string {
	return strings.Replace(s.URL, "http", "ws", 1)
}
