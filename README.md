# websocket

[![GoDoc](https://godoc.org/nhooyr.io/websocket?status.svg)](https://godoc.org/nhooyr.io/websocket)

websocket is a minimal and idiomatic WebSocket library for Go.

This library is in heavy development.

## Install

```bash
go get nhooyr.io/websocket
```

## Features

- Full support of the WebSocket protocol
- Only depends on stdlib
- Simple to use
- context.Context is first class
- net/http is used for WebSocket dials and upgrades
- Thoroughly tested, fully passes the [autobahn-testsuite](https://github.com/crossbario/autobahn-testsuite)
- JSON helpers

## Roadmap

- [ ] WebSockets over HTTP/2 [#4](https://github.com/nhooyr/websocket/issues/4)
- [ ] Deflate extension support [#5](https://github.com/nhooyr/websocket/issues/5)
- [ ] More optimization [#11](https://github.com/nhooyr/websocket/issues/11)
- [ ] WASM [#15](https://github.com/nhooyr/websocket/issues/15)
- [ ] Ping/pongs [#1](https://github.com/nhooyr/websocket/issues/1)

## Example

### Server

```go
fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r)
	if err != nil {
		log.Printf("server handshake failed: %v", err)
		return
	}
	defer c.Close(websocket.StatusInternalError, "")

	jc := websocket.JSONConn{
		Conn: c,
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Second*10)
	defer cancel()

	v := map[string]interface{}{
		"my_field": "foo",
	}
	err = jc.Write(ctx, v)
	if err != nil {
		log.Printf("failed to write json: %v", err)
		return
	}

	log.Printf("wrote %v", v)

	c.Close(websocket.StatusNormalClosure, "")
})
err := http.ListenAndServe("localhost:8080", fn)
if err != nil {
	log.Fatalf("failed to listen and serve: %v", err)
}
```

For a production quality example that shows off the low level API, see the [echo server example](https://github.com/nhooyr/websocket/blob/master/example_test.go#L15).

### Client

```go
ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
defer cancel()

c, _, err := websocket.Dial(ctx, "ws://localhost:8080")
if err != nil {
	log.Fatalf("failed to ws dial: %v", err)
}
defer c.Close(websocket.StatusInternalError, "")

jc := websocket.JSONConn{
	Conn: c,
}

var v interface{}
err = jc.Read(ctx, v)
if err != nil {
	log.Fatalf("failed to read json: %v", err)
}

log.Printf("received %v", v)

c.Close(websocket.StatusNormalClosure, "")
```

## Design considerations

- Minimal API is easier to maintain and for others to learn
- Context based cancellation is more ergonomic and robust than setting deadlines
- No pings or pongs because TCP keep alives work fine for HTTP/1.1 and they do not make
  sense with HTTP/2 (see #1)
- net.Conn is never exposed as WebSocket's over HTTP/2 will not have a net.Conn.
- Functional options make the API very clean and easy to extend
- Using net/http's Client for dialing means we do not have to reinvent dialing hooks
  and configurations. Just pass in a custom net/http client if you want custom dialing.

## Comparison

While I believe nhooyr/websocket has a better API than existing libraries, 
both gorilla/websocket and gobwas/ws were extremely useful in implementing the
WebSocket protocol correctly so big thanks to the authors of both. In particular,
I made sure to go through the issue tracker of gorilla/websocket to make sure
I implemented details correctly.

### gorilla/websocket

https://github.com/gorilla/websocket

This package is the community standard but it is very old and over time
has accumulated cruft. There are many ways to do the same thing and the API
is not clear. Just compare the godoc of
[nhooyr/websocket](godoc.org/github.com/nhooyr/websocket) side by side with
[gorilla/websocket](godoc.org/github.com/gorilla/websocket).

The API for nhooyr/websocket has been designed such that there is only one way to do things
which makes using it correctly and safely much easier.

In terms of lines of code, this library is around 2000 whereas gorilla/websocket is
at 7000. So while the API for nhooyr/websocket is simpler, the implementation is also
significantly simpler and easier to test which reduces the surface are of bugs.

Furthermore, nhooyr/websocket has support for newer Go idioms such as context.Context and
also uses net/http's Client and ResponseWriter directly for WebSocket handshakes.
gorilla/websocket writes its handshakes directly to a net.Conn which means
it has to reinvent hooks for TLS and proxying and prevents support of HTTP/2.

### x/net/websocket

https://godoc.org/golang.org/x/net/websocket

Unmaintained and the API does not reflect WebSocket semantics. Should never be used.

See https://github.com/golang/go/issues/18152

### gobwas/ws

https://github.com/gobwas/ws

This library has an extremely flexible API but that comes at the cost of usability
and clarity. Its not clear what the best way to do anything is.

This library is fantastic in terms of performance. The author put in significant
effort to ensure its speed and I have applied as many of its optimizations as
I could into nhooyr/websocket.

If you want a library that gives you absolute control over everything, this is the library,
but for most users, the API provided by nhooyr/websocket will definitely fit better as it will
be just as performant but much easier to use correctly.
