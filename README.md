# websocket

[![godoc](https://godoc.org/nhooyr.io/websocket?status.svg)](https://pkg.go.dev/nhooyr.io/websocket)
[![coverage](https://img.shields.io/badge/coverage-88%25-success)](https://nhooyrio-websocket-coverage.netlify.app)

websocket is a minimal and idiomatic WebSocket library for Go.

## Table of Contents

1. [Install](#install)
1. [Highlights](#highlights)
1. [Roadmap](#roadmap)
1. [Examples](#examples)
1. [Comparison With Other Packages](#comparison-with-other-packages)
1. [Ping/Pong](#pingpong)

## Install

```bash
go get nhooyr.io/websocket
```

## Highlights

- Minimal and idiomatic API
- First class [context.Context](https://blog.golang.org/context) support
- Fully passes the WebSocket [autobahn-testsuite](https://github.com/crossbario/autobahn-testsuite)
- [Single dependency](https://pkg.go.dev/nhooyr.io/websocket?tab=imports)
- JSON and protobuf helpers in the [wsjson](https://pkg.go.dev/nhooyr.io/websocket/wsjson) and [wspb](https://pkg.go.dev/nhooyr.io/websocket/wspb) subpackages
- Zero alloc reads and writes
- Concurrent writes
- [Close handshake](https://pkg.go.dev/nhooyr.io/websocket#Conn.Close)
- [net.Conn](https://pkg.go.dev/nhooyr.io/websocket#NetConn) wrapper
- [Ping pong](https://pkg.go.dev/nhooyr.io/websocket#Conn.Ping) API
- [RFC 7692](https://tools.ietf.org/html/rfc7692) permessage-deflate compression
- Compile to [Wasm](https://pkg.go.dev/nhooyr.io/websocket#hdr-Wasm)

## Roadmap

- [ ] HTTP/2 [#4](https://github.com/nhooyr/websocket/issues/4)

## Examples

For a production quality example that demonstrates the complete API, see the
[echo example](./examples/echo).

For a full stack example, see the [chat example](./examples/chat).

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

## Comparison With Other Packages

### gorilla/websocket

Advantages of [gorilla/websocket](https://github.com/gorilla/websocket):

- Mature and widely used
- [Prepared writes](https://pkg.go.dev/github.com/gorilla/websocket#PreparedMessage)
- Configurable [buffer sizes](https://pkg.go.dev/github.com/gorilla/websocket#hdr-Buffers)

Advantages of nhooyr.io/websocket:

- Minimal and idiomatic API
  - Compare godoc of [nhooyr.io/websocket](https://pkg.go.dev/nhooyr.io/websocket) with [gorilla/websocket](https://pkg.go.dev/github.com/gorilla/websocket) side by side.
- [net.Conn](https://pkg.go.dev/nhooyr.io/websocket#NetConn) wrapper
- Zero alloc reads and writes ([gorilla/websocket#535](https://github.com/gorilla/websocket/issues/535))
- Full [context.Context](https://blog.golang.org/context) support
- Dial uses [net/http.Client](https://golang.org/pkg/net/http/#Client)
  - Will enable easy HTTP/2 support in the future
  - Gorilla writes directly to a net.Conn and so duplicates features of net/http.Client.
- Concurrent writes
- Close handshake ([gorilla/websocket#448](https://github.com/gorilla/websocket/issues/448))
- Idiomatic [ping pong](https://pkg.go.dev/nhooyr.io/websocket#Conn.Ping) API
  - Gorilla requires registering a pong callback before sending a Ping
- Can target Wasm ([gorilla/websocket#432](https://github.com/gorilla/websocket/issues/432))
- Transparent message buffer reuse with [wsjson](https://pkg.go.dev/nhooyr.io/websocket/wsjson) and [wspb](https://pkg.go.dev/nhooyr.io/websocket/wspb) subpackages
- [1.75x](https://github.com/nhooyr/websocket/releases/tag/v1.7.4) faster WebSocket masking implementation in pure Go
  - Gorilla's implementation is slower and uses [unsafe](https://golang.org/pkg/unsafe/).
- Full [permessage-deflate](https://tools.ietf.org/html/rfc7692) compression extension support
  - Gorilla only supports no context takeover mode
  - We use [klauspost/compress](https://github.com/klauspost/compress) for much lower memory usage ([gorilla/websocket#203](https://github.com/gorilla/websocket/issues/203))
- [CloseRead](https://pkg.go.dev/nhooyr.io/websocket#Conn.CloseRead) helper ([gorilla/websocket#492](https://github.com/gorilla/websocket/issues/492))
- Actively maintained ([gorilla/websocket#370](https://github.com/gorilla/websocket/issues/370))

#### golang.org/x/net/websocket

[golang.org/x/net/websocket](https://pkg.go.dev/golang.org/x/net/websocket) is deprecated.
See [golang/go/issues/18152](https://github.com/golang/go/issues/18152).

The [net.Conn](https://pkg.go.dev/nhooyr.io/websocket#NetConn) can help in transitioning
to nhooyr.io/websocket.

#### gobwas/ws

[gobwas/ws](https://github.com/gobwas/ws) has an extremely flexible API that allows it to be used
in an event driven style for performance. See the author's [blog post](https://medium.freecodecamp.org/million-websockets-and-go-cc58418460bb).

However when writing idiomatic Go, nhooyr.io/websocket will be faster and easier to use.

## Ping/Pong

Some WebSocket libraries and WebSocket applications implement pings and pongs. Is this necessary?

The WebSocket protocol (layer 7) is built on top of the HTTP protocol (layer 7), which is built on top of the TCP protocol (layer 4). TCP has a feature called [keepalive](https://tldp.org/HOWTO/TCP-Keepalive-HOWTO/overview.html), which, as its name suggests, keeps long connections alive. keepalive is automatically enabled in the Golang standard standard library, so any HTTP server that you create with Golang should inherit it. By default, it [sends probes every 15 seconds and has a timeout of 3 minutes](https://golang.org/pkg/net/#Dialer).

WebSocket applications almost always need to detect when an end-user's internet has died, so that they can properly disconnect the user and clean up the relevant data structures. Using TCP keepalives for this purpose should be sufficient - meaning that we don't need to write any additional code.

Some Golang authors, such as the creators of the [melody WebSocket framework](https://github.com/olahol/melody), have implemented WebSocket-level ping/pong code inside the framework as an extra failsafe in case that TCP keepalives aren't working. Additionally, [some people claim](https://github.com/nhooyr/websocket/issues/265#issuecomment-734095675) that TCP keepalives don't work well when going through proxy servers, which would seem to justify this extra code.

However, reliable data for TCP keepalives not working properly is scant. After [careful consideration](https://github.com/nhooyr/websocket/issues/1), this library does not include automatic ping/pong code like the melody WebSocket framework does. Until more evidence is gathered either way, the authors of this library do not recommend putting pong/pong code in your application. However, we always welcome [more discussion on the topic](https://github.com/nhooyr/websocket/issues/1).
