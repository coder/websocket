# Echo Example

This directory contains a echo server example using nhooyr.io/websocket.

```bash
$ cd examples/echo
$ go run . localhost:0
listening on http://127.0.0.1:51055
```

You can use a WebSocket client like https://github.com/hashrocket/ws to connect. All messages
written will be echoed back.

## Structure

The server is in `server.go` and is implemented as a `http.HandlerFunc` that accepts the WebSocket
and then reads all messages and writes them exactly as is back to the connection.

`server_test.go` contains a small unit test to verify it works correctly.

`main.go` brings it all together so that you can run it and play around with it.
