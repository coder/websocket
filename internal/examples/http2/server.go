package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/coder/websocket"
)

// serverMain starts a minimal HTTP/2 WebSocket echo server.
// - By default it serves h2c (cleartext HTTP/2): ws://
// - With -tls it serves TLS+HTTP/2: wss:// (requires -cert and -key)
func serverMain(prog string, args []string) error {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "address to listen on (host:port)")
	useTLS := fs.Bool("tls", false, "enable TLS (wss://)")
	certFile := fs.String("cert", "", "path to TLS certificate (PEM)")
	keyFile := fs.String("key", "", "path to TLS private key (PEM)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage:
  %[1]s server [options]

Options:
`, prog)
		fs.PrintDefaults()
		fmt.Fprintf(fs.Output(), `
Examples:
  GODEBUG=http2xconnect=1 %[1]s server -addr :8080
  GODEBUG=http2xconnect=1 %[1]s server -tls -cert cert.pem -key key.pem -addr :8443
`, prog)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !strings.Contains(os.Getenv("GODEBUG"), "http2xconnect=1") {
		return errors.New("http2xconnect is not enabled, please set GODEBUG=http2xconnect=1")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			HTTPProtocol: websocket.HTTPProtocol2,
		})
		if err != nil {
			// Accept already wrote an error response.
			return
		}
		defer c.CloseNow()

		for {
			typ, rr, err := c.Reader(ctx)
			if err != nil {
				// Graceful close by client.
				if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
					return
				}
				return
			}
			ww, err := c.Writer(ctx, typ)
			if err != nil {
				return
			}
			if _, err = io.Copy(ww, rr); err != nil {
				_ = ww.Close()
				return
			}
			if err = ww.Close(); err != nil {
				return
			}
		}
	})

	srv := &http.Server{
		Addr:    *addr,
		Handler: mux,
	}

	if *useTLS {
		if *certFile == "" || *keyFile == "" {
			return errors.New("-cert and -key are required when using -tls")
		}

		// Enable HTTP/2 over TLS.
		if err := http2.ConfigureServer(srv, &http2.Server{}); err != nil {
			return err
		}

		fmt.Printf("listening on wss://%s\n", visibleAddr(*addr))
		return srv.ListenAndServeTLS(*certFile, *keyFile)
	}

	// Cleartext HTTP/2 (h2c).
	srv.Handler = h2c.NewHandler(srv.Handler, &http2.Server{})
	fmt.Printf("listening on ws://%s (h2c)\n", visibleAddr(*addr))
	return srv.ListenAndServe()
}

func visibleAddr(addr string) string {
	// If binding to all interfaces with ":port", display "0.0.0.0:port".
	if strings.HasPrefix(addr, ":") {
		return "0.0.0.0" + addr
	}
	return addr
}
