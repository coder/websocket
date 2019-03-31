# websocket

[![GoDoc](https://godoc.org/nhooyr.io/websocket?status.svg)](https://godoc.org/nhooyr.io/websocket)
[![Codecov](https://img.shields.io/codecov/c/github/nhooyr/websocket.svg)](https://codecov.io/gh/nhooyr/websocket)
[![GitHub release](https://img.shields.io/github/release/nhooyr/websocket.svg)](https://github.com/nhooyr/websocket/releases)

websocket is a minimal and idiomatic WebSocket library for Go.

This library is in heavy development.

## Install

```bash
go get nhooyr.io/websocket@master
```

## Features

- Full support of the WebSocket protocol
- Only depends on the stdlib
- Simple to use because of the minimal API
- Uses the context package for cancellation
- Uses net/http's Client to do WebSocket dials
- Compression of text frames larger than 1024 bytes by default
- Highly optimized
- API will transparently work with WebSockets over HTTP/2
- WASM support

## Example

### Server

```go
func main() {
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r,
			websocket.AcceptSubprotocols("test"),
		)
		if err != nil {
			log.Printf("server handshake failed: %v", err)
			return
		}
		defer c.Close(websocket.StatusInternalError, "")

		ctx, cancel := context.WithTimeout(r.Context(), time.Second*10)
		defer cancel()

		v := map[string]interface{}{
			"my_field": "foo",
		}
		err = websocket.WriteJSON(ctx, c, v)
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
 }
```

For a production quality example that shows off the low level API, see the echo example on the [godoc](https://godoc.org/nhooyr.io/websocket#Accept).

### Client

```go
func main() {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	c, _, err := websocket.Dial(ctx, "ws://localhost:8080",
		websocket.DialSubprotocols("test"),
	)
	if err != nil {
		log.Fatalf("failed to ws dial: %v", err)
	}
	defer c.Close(websocket.StatusInternalError, "")

	var v interface{}
	err = websocket.ReadJSON(ctx, c, v)
	if err != nil {
		log.Fatalf("failed to read json: %v", err)
	}

	log.Printf("received %v", v)

	c.Close(websocket.StatusNormalClosure, "")
}
```

See [example_test.go](example_test.go) for more examples.

## Design considerations

- Minimal API is easier to maintain and for others to learn
- Context based cancellation is more ergonomic and robust than setting deadlines
- No pings or pongs because TCP keep alives work fine for HTTP/1.1 and they do not make
  sense with HTTP/2
- net.Conn is never exposed as WebSocket's over HTTP/2 will not have a net.Conn.
- Functional options make the API very clean and easy to extend
- Compression is very useful for JSON payloads
- Protobuf and JSON helpers make code terse
- Using net/http's Client for dialing means we do not have to reinvent dialing hooks
  and configurations. Just pass in a custom net/http client if you want custom dialing.

## Comparison

While I believe nhooyr/websocket has a better API than existing libraries, 
both gorilla/websocket and gobwas/ws were extremely useful in implementing the
WebSocket protocol correctly so big thanks to the authors of both.

### gorilla/websocket

https://github.com/gorilla/websocket

This package is the community standard but it is very old and over timennn
has accumulated cruft. There are many ways to do the same thing and the API
overall is just not very clear. Just compare the godoc of
[nhooyr/websocket](godoc.org/github.com/nhooyr/websocket) side by side with
[gorilla/websocket](godoc.org/github.com/gorilla/websocket).

The API for nhooyr/websocket has been designed such that there is only one way to do things
and with HTTP/2 in mind which makes using it correctly and safely much easier.

### x/net/websocket

https://godoc.org/golang.org/x/net/websocket

Unmaintained and the API does not reflect WebSocket semantics. Should never be used.

See https://github.com/golang/go/issues/18152

### gobwas/ws

https://github.com/gobwas/ws

This library has an extremely flexible API but that comes at the cost of usability
and clarity. Its just not clear how to do things in a safe manner. 

This library is fantastic in terms of performance though. The author put in significant
effort to ensure its speed and I have tried to apply as many of its teachings as
I could into nhooyr/websocket.

If you want a library that gives you absolute control over everything, this is the library,
but for most users, the API provided by nhooyr/websocket will definitely fit better as it will
be just as performant but much easier to use.
