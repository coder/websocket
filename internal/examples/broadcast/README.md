# Broadcast Example

This directory contains a broadcast server example using github.com/coder/websocket.

```bash
$ cd internal/examples/broadcast
$ go run . localhost:8080
listening on http://127.0.0.1:8080
```

Subscribe with a WebSocket client like [websocat](https://github.com/vi/websocat) and publish
messages with curl:

```bash
$ websocat ws://127.0.0.1:8080/subscribe
$ curl --data-binary 'hello' http://127.0.0.1:8080/publish
```

Every published message is delivered to all subscribers. Messages are sent as text, so
`/publish` rejects bodies that are not valid UTF-8.

## Structure

The server is in `server.go`. Subscribers connect to the WebSocket `/subscribe` endpoint and
messages are published via the HTTP POST `/publish` endpoint, so that you can easily publish
with curl.

Each published message is wrapped in a single `PreparedMessage` and queued for every subscriber.
Connections are accepted with `CompressionNoContextTakeover`, so a message that reaches the
compression threshold is compressed once per broadcast instead of once per subscriber.
Subscribers that do not support compression receive the same message uncompressed.

Publishing never blocks: a subscriber that cannot keep up with its queue is disconnected.

`server_test.go` contains a test that subscribes clients with and without compression and
ensures every client receives every published message, while invalid UTF-8 is rejected.

`main.go` brings it all together so that you can run it and play around with it.
