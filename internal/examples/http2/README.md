# HTTP/2 WebSocket Example

This example shows a minimal WebSocket echo over HTTP/2 using extended CONNECT
(RFC 8441) with `github.com/coder/websocket`.

It supports:

- h2c (cleartext HTTP/2) via `ws://`
- TLS + HTTP/2 via `wss://` (self-signed by default, or bring your own cert/key)

## Run

Cleartext HTTP/2:

```console
# Server.
$ cd examples/http2
$ GODEBUG=http2xconnect=1 go run . server -addr :8080
listening on ws://127.0.0.1:8080 (h2c)

# Client.
$ go run . client ws://127.0.0.1:8080
Hello over HTTP/2 WebSocket!
```

TLS (wss) with self-signed cert:

```console
# Server.
$ cd examples/http2
$ GODEBUG=http2xconnect=1 go run . server -tls -addr :8443
listening on wss://127.0.0.1:8443 (self-signed)

# Client.
$ go run . client -insecure wss://localhost:8443
Hello over HTTP/2 WebSocket!
```

TLS (wss) with your own cert/key:

```console
# Server.
$ cd examples/http2
$ GODEBUG=http2xconnect=1 go run . server -tls -cert cert.pem -key key.pem -addr :8443
listening on wss://127.0.0.1:8443 (cert/key)

# Client.
$ go run . client wss://your.host:8443
```

## Structure

The server is in `server.go` and is implemented as an `http.Handler` that
accepts a WebSocket over HTTP/2 (extended CONNECT) and echoes messages. It
supports cleartext HTTP/2 (h2c) and TLS; for TLS it can generate a self‑signed
certificate or use a provided cert/key.

The client is in `client.go`. It dials the server over HTTP/2 (both `ws://` h2c
and `wss://` TLS), sends a single text message, and prints the echoed response.

`main.go` wires a small CLI with `server` and `client` subcommands so you can
run and try the example quickly.
