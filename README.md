# websocket

[![version](https://img.shields.io/github/v/release/nhooyr/websocket?color=6b9ded&sort=semver)](https://github.com/nhooyr/websocket/releases)
[![docs](https://godoc.org/nhooyr.io/websocket?status.svg)](https://godoc.org/nhooyr.io/websocket)
[![coverage](https://img.shields.io/coveralls/github/nhooyr/websocket?color=65d6a4)](https://coveralls.io/github/nhooyr/websocket)
[![ci](https://github.com/nhooyr/websocket/workflows/ci/badge.svg)](https://github.com/nhooyr/websocket/actions)

websocket is a minimal and idiomatic WebSocket library for Go.

## Install

```bash
go get nhooyr.io/websocket
```

## Features

- Minimal and idiomatic API
- First class [context.Context](https://blog.golang.org/context) support
- Thorough tests, fully passes the [autobahn-testsuite](https://github.com/crossbario/autobahn-testsuite)
- [Zero dependencies](https://godoc.org/nhooyr.io/websocket?imports)
- JSON and ProtoBuf helpers in the [wsjson](https://godoc.org/nhooyr.io/websocket/wsjson) and [wspb](https://godoc.org/nhooyr.io/websocket/wspb) subpackages
- Zero alloc reads and writes
- Concurrent writes
- [Close handshake](https://godoc.org/nhooyr.io/websocket#Conn.Close)
- [net.Conn](https://godoc.org/nhooyr.io/websocket#NetConn) wrapper
- [Pings](https://godoc.org/nhooyr.io/websocket#Conn.Ping)
- [RFC 7692](https://tools.ietf.org/html/rfc7692) permessage-deflate compression
- Compile to [Wasm](https://godoc.org/nhooyr.io/websocket#hdr-Wasm)

## Roadmap

- [ ] HTTP/2 [#4](https://github.com/nhooyr/websocket/issues/4)

## Examples

For a production quality example that demonstrates the full API, see the [echo example](https://godoc.org/nhooyr.io/websocket#example-package--Echo).

### Server

```go
http.HandlerFunc(func (w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		// ...
	}
	defer c.Close(websocket.StatusInternalError, "the sky is falling")

	ctx, cancel := context.WithTimeout(r.Context(), time.Second*10)
	defer cancel()

	var v interface{}
	err = wsjson.Read(ctx, c, &v)
	if err != nil {
		// ...
	}

	log.Printf("received: %v", v)

	c.Close(websocket.StatusNormalClosure, "")
})
```

### Client

```go
ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
defer cancel()

c, _, err := websocket.Dial(ctx, "ws://localhost:8080", nil)
if err != nil {
	// ...
}
defer c.Close(websocket.StatusInternalError, "the sky is falling")

err = wsjson.Write(ctx, c, "hi")
if err != nil {
	// ...
}

c.Close(websocket.StatusNormalClosure, "")
```

## Comparison

### gorilla/websocket

[gorilla/websocket](https://github.com/gorilla/websocket) is a widely used and mature library.

Advantages of nhooyr.io/websocket:
  - Minimal and idiomatic API
    - Compare godoc of [nhooyr.io/websocket](https://godoc.org/nhooyr.io/websocket) with [gorilla/websocket](https://godoc.org/github.com/gorilla/websocket) side by side.
  - [net.Conn](https://godoc.org/nhooyr.io/websocket#NetConn) wrapper
  - Zero alloc reads and writes ([gorilla/websocket#535](https://github.com/gorilla/websocket/issues/535))
  - Full [context.Context](https://blog.golang.org/context) support
  - Uses [net/http.Client](https://golang.org/pkg/net/http/#Client) for dialing
    - Will enable easy HTTP/2 support in the future
    - Gorilla writes directly to a net.Conn and so duplicates features from net/http.Client.
  - Concurrent writes
  - Close handshake ([gorilla/websocket#448](https://github.com/gorilla/websocket/issues/448))
  - Idiomatic [ping](https://godoc.org/nhooyr.io/websocket#Conn.Ping) API
    - gorilla/websocket requires registering a pong callback and then sending a Ping
  - Wasm ([gorilla/websocket#432](https://github.com/gorilla/websocket/issues/432))
  - Transparent buffer reuse with [wsjson](https://godoc.org/nhooyr.io/websocket/wsjson) and [wspb](https://godoc.org/nhooyr.io/websocket/wspb) subpackages
  - [1.75x](https://github.com/nhooyr/websocket/releases/tag/v1.7.4) faster WebSocket masking implementation in pure Go
    - Gorilla's implementation depends on unsafe and is slower
  - Full [permessage-deflate](https://tools.ietf.org/html/rfc7692) compression extension support
  - Gorilla only supports no context takeover mode
  - [CloseRead](https://godoc.org/nhooyr.io/websocket#Conn.CloseRead) helper
  - Actively maintained ([gorilla/websocket#370](https://github.com/gorilla/websocket/issues/370))

#### golang.org/x/net/websocket

[golang.org/x/net/websocket](https://godoc.org/golang.org/x/net/websocket) is deprecated.
See ([golang/go/issues/18152](https://github.com/golang/go/issues/18152)).

The [net.Conn](https://godoc.org/nhooyr.io/websocket#NetConn) wrapper will ease in transitioning
to nhooyr.io/websocket.

#### gobwas/ws

[gobwas/ws](https://github.com/gobwas/ws) has an extremely flexible API that allows it to be used
in an event driven style for performance. See the author's [blog post](https://medium.freecodecamp.org/million-websockets-and-go-cc58418460bb). 

However when writing idiomatic Go, nhooyr.io/websocket will be faster and easier to use.

## Users

If your company or project is using this library, feel free to open an issue or PR to amend this list.

- [Coder](https://github.com/cdr)
- [Tatsu Works](https://github.com/tatsuworks) - Ingresses 20 TB in WebSocket data every month on their Discord bot.
