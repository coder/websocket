//go:build !js
// +build !js

package websocket

import (
	"context"
	"net/http"
)

func Dial(ctx context.Context, u string, opts *DialOptions) (*Conn, *http.Response, error) {
	return dialStd(ctx, u, opts, nil)
}
