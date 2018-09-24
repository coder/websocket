# ws

[![GoDoc](https://godoc.org/nhooyr.io/ws?status.svg)](https://godoc.org/nhooyr.io/ws)

An orthogonal and idiomatic WebSocket library for Go.

This library is in heavy development.

## Install

```
go get nhooyr.io/ws
```

## API Justification

- Deadlines are per message reader or writer instead of being connection wide which allows us to allow concurrent writes.

- We always ping/pong for simplicity. Allows us to completely remove an API for ping/pongs. I do not see a legitimate use case
where one needs manual access to ping/pongs.

- No prepared frames because it only gives performance boost for clients due to masking but masking is heavily optimized
and most clients will not be sending a single message to more than one server.

- Server handshake method is really simple and requires no callbacks or options. Non essential headers must be handled
by consumers of the package. Likewise with the client handshake.

- We always buffer read and writes. I do not think this needs to be configurable as net/http and x/net/http2 do not make
it configurable either. A solid default is enough.

- 
