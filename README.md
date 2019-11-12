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
- Tiny codebase at 2200 lines
- First class [context.Context](https://blog.golang.org/context) support
- Thorough tests, fully passes the [autobahn-testsuite](https://github.com/crossbario/autobahn-testsuite)
- [Zero dependencies](https://godoc.org/nhooyr.io/websocket?imports)
- JSON and ProtoBuf helpers in the [wsjson](https://godoc.org/nhooyr.io/websocket/wsjson) and [wspb](https://godoc.org/nhooyr.io/websocket/wspb) subpackages
- Highly optimized by default
  - Zero alloc reads and writes
- Concurrent writes out of the box
- [Complete Wasm](https://godoc.org/nhooyr.io/websocket#hdr-Wasm) support
- [Close handshake](https://godoc.org/nhooyr.io/websocket#Conn.Close)
- Full support of [RFC 7692](https://tools.ietf.org/html/rfc7692) permessage-deflate compression extension

## Roadmap

- [ ] HTTP/2 [#4](https://github.com/nhooyr/websocket/issues/4)

## Examples

For a production quality example that shows off the full API, see the [echo example on the godoc](https://godoc.org/nhooyr.io/websocket#example-package--Echo). On github, the example is at [example_echo_test.go](./example_echo_test.go).

Use the [errors.As](https://golang.org/pkg/errors/#As) function [new in Go 1.13](https://golang.org/doc/go1.13#error_wrapping) to check for [websocket.CloseError](https://godoc.org/nhooyr.io/websocket#CloseError).
There is also [websocket.CloseStatus](https://godoc.org/nhooyr.io/websocket#CloseStatus) to quickly grab the close status code out of a [websocket.CloseError](https://godoc.org/nhooyr.io/websocket#CloseError).
See the [CloseStatus godoc example](https://godoc.org/nhooyr.io/websocket#example-CloseStatus).

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

Before the comparison, I want to point out that gorilla/websocket was extremely useful in implementing the
WebSocket protocol correctly so _big thanks_ to its authors. In particular, I made sure to go through the
issue tracker of gorilla/websocket to ensure I implemented details correctly and understood how people were
using WebSockets in production.

### gorilla/websocket

https://github.com/gorilla/websocket

The implementation of gorilla/websocket is 6 years old. As such, it is
widely used and very mature compared to nhooyr.io/websocket.

On the other hand, it has grown organically and now there are too many ways to do
the same thing. Compare the godoc of
[nhooyr/websocket](https://godoc.org/nhooyr.io/websocket) with
[gorilla/websocket](https://godoc.org/github.com/gorilla/websocket) side by side.

The API for nhooyr.io/websocket has been designed such that there is only one way to do things.
This makes it easy to use correctly. Not only is the API simpler, the implementation is
only 2200 lines whereas gorilla/websocket is at 3500 lines. That's more code to maintain,
more code to test, more code to document and more surface area for bugs.

Moreover, nhooyr.io/websocket supports newer Go idioms such as context.Context.
It also uses net/http's Client and ResponseWriter directly for WebSocket handshakes.
gorilla/websocket writes its handshakes to the underlying net.Conn.
Thus it has to reinvent hooks for TLS and proxies and prevents easy support of HTTP/2.

Some more advantages of nhooyr.io/websocket are that it supports concurrent writes and
makes it very easy to close the connection with a status code and reason. In fact,
nhooyr.io/websocket even implements the complete WebSocket close handshake for you whereas
with gorilla/websocket you have to perform it manually. See [gorilla/websocket#448](https://github.com/gorilla/websocket/issues/448).

The ping API is also nicer. gorilla/websocket requires registering a pong handler on the Conn
which results in awkward control flow. With nhooyr.io/websocket you use the Ping method on the Conn
that sends a ping and also waits for the pong.

Additionally, nhooyr.io/websocket can compile to [Wasm](https://godoc.org/nhooyr.io/websocket#hdr-Wasm) for the browser.

In terms of performance, the differences mostly depend on your application code. nhooyr.io/websocket
reuses message buffers out of the box if you use the wsjson and wspb subpackages.
As mentioned above, nhooyr.io/websocket also supports concurrent writers.

The WebSocket masking algorithm used by this package is [1.75x](https://github.com/nhooyr/websocket/releases/tag/v1.7.4)
faster than gorilla/websocket while using only pure safe Go.

The [permessage-deflate compression extension](https://tools.ietf.org/html/rfc7692) is fully supported by this library
whereas gorilla only supports no context takeover mode. See our godoc for the differences. This will make a big
difference on bandwidth used in most use cases.

The only performance con to nhooyr.io/websocket is that it uses a goroutine to support
cancellation with context.Context. This costs 2 KB of memory which is cheap compared to
the benefits.

### x/net/websocket

https://godoc.org/golang.org/x/net/websocket

Unmaintained and the API does not reflect WebSocket semantics. Should never be used.

See https://github.com/golang/go/issues/18152

### gobwas/ws

https://github.com/gobwas/ws

This library has an extremely flexible API but that comes at the cost of usability
and clarity.

Due to its flexibility, it can be used in a event driven style for performance.
Definitely check out his fantastic [blog post](https://medium.freecodecamp.org/million-websockets-and-go-cc58418460bb) about performant WebSocket servers.

If you want a library that gives you absolute control over everything, this is the library.
But for 99.9% of use cases, nhooyr.io/websocket will fit better as it is both easier and
faster for normal idiomatic Go. The masking implementation is [1.75x](https://github.com/nhooyr/websocket/releases/tag/v1.7.4)
faster, the compression extensions are fully supported and as much as possible is reused by default.

See the gorilla/websocket comparison for more performance details.

## Contributing

See [.github/CONTRIBUTING.md](.github/CONTRIBUTING.md).

## Users

If your company or project is using this library, feel free to open an issue or PR to amend this list.

- [Coder](https://github.com/cdr)
- [Tatsu Works](https://github.com/tatsuworks) - Ingresses 20 TB in websocket data every month on their Discord bot.
