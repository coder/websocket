// +build !js

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"

	"nhooyr.io/websocket/internal/wsecho"
)

func main() {
	s := httptest.NewServer(http.HandlerFunc(wsecho.Serve))
	wsURL := strings.Replace(s.URL, "http", "ws", 1)
	fmt.Printf("%v\n", wsURL)

	runtime.Goexit()
}
