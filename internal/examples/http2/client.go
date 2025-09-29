package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/http2"

	"github.com/coder/websocket"
)

// clientMain implements a minimal HTTP/2 (extended CONNECT) WebSocket client.
// It supports:
// - ws:// using h2c (cleartext HTTP/2)
// - wss:// using TLS HTTP/2 (with optional -insecure)
func clientMain(prog string, args []string) error {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	insecure := fs.Bool("insecure", false, "skip TLS verification for wss://")
	message := fs.String("message", "Hello over HTTP/2 WebSocket!", "message to send")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage:
  %[1]s client [options] <ws|wss URL>

Options:
`, prog)
		fs.PrintDefaults()
		fmt.Fprintf(fs.Output(), `
Examples:
  %[1]s client ws://127.0.0.1:8080
  %[1]s client -insecure wss://localhost:8443
`, prog)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("missing URL")
	}
	rawURL := fs.Arg(0)

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return fmt.Errorf("unsupported scheme %q: use ws:// or wss://", u.Scheme)
	}

	// Build an HTTP/2-capable client suitable for the scheme.
	var hc *http.Client
	if u.Scheme == "ws" {
		// Cleartext HTTP/2 (h2c).
		h2t := &http2.Transport{
			AllowHTTP: true,
			DialTLS: func(network, addr string, _ *tls.Config) (net.Conn, error) {
				return net.Dial(network, addr)
			},
		}
		hc = &http.Client{Transport: h2t}
	} else {
		// TLS HTTP/2.
		h2t := &http2.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: *insecure,
			},
		}
		hc = &http.Client{Transport: h2t}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, rawURL, &websocket.DialOptions{
		HTTPClient: hc,
		Protocol:   websocket.ProtocolHTTP2,
	})
	if err != nil {
		if resp != nil {
			b, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("dial failed: %w; response: %s", err, string(b))
		}
		return fmt.Errorf("dial failed: %w", err)
	}
	defer conn.CloseNow()

	// Send one text message and print the echoed response.
	w, err := conn.Writer(ctx, websocket.MessageText)
	if err != nil {
		return err
	}
	if _, err = io.WriteString(w, *message); err != nil {
		_ = w.Close()
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}

	typ, r, err := conn.Reader(ctx)
	if err != nil {
		return err
	}
	if typ != websocket.MessageText {
		return fmt.Errorf("unexpected message type: %v", typ)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	fmt.Println(string(b))

	_ = conn.Close(websocket.StatusNormalClosure, "bye")
	return nil
}
