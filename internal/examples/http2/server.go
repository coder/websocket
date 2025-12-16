package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/coder/websocket"
)

// serverMain starts a minimal HTTP/2 WebSocket echo server.
// - By default it serves h2c (cleartext HTTP/2): ws://
// - With -tls it serves TLS+HTTP/2: wss://
//   - If -cert and -key are not provided, a self-signed certificate is generated.
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
  GODEBUG=http2xconnect=1 %[1]s server -tls -addr :8443
  GODEBUG=http2xconnect=1 %[1]s server -tls -cert cert.pem -key key.pem
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
			Protocol: websocket.ProtocolHTTP2,
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
		// Enable HTTP/2 over TLS.
		if err := http2.ConfigureServer(srv, &http2.Server{}); err != nil {
			return err
		}

		selfSigned := ""
		if *certFile == "" && *keyFile == "" {
			// No certificate provided, generate an
			// ephemeral self-signed certificate.
			cert, err := selfSignedRSA2048()
			if err != nil {
				return fmt.Errorf("generate self-signed certificate failed: %w", err)
			}
			srv.TLSConfig.Certificates = []tls.Certificate{cert}
			selfSigned = " (self-signed)"
		}

		fmt.Printf("listening on wss://%s%s\n", visibleAddr(*addr), selfSigned)
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

// selfSignedRSA2048 returns an ephemeral self-signed certificate
// suitable for TLS servers.
//
// DO NOT USE IN PRODUCTION.
func selfSignedRSA2048() (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate private key failed: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate serial number failed: %w", err)
	}

	tpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "websocket-example",
			Organization: []string{"websocket-example"},
		},
		DNSNames:              []string{"localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &tpl, &tpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create certificate failed: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	return tls.X509KeyPair(certPEM, keyPEM)
}
