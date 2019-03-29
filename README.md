# ws

[![GoDoc](https://godoc.org/nhooyr.io/ws?status.svg)](https://godoc.org/nhooyr.io/ws)
[![Codecov](https://img.shields.io/codecov/c/github/nhooyr/ws.svg)](https://codecov.io/gh/nhooyr/ws)
[![GitHub release](https://img.shields.io/github/release/nhooyr/ws.svg)](https://github.com/nhooyr/ws/releases)

ws is a clean and idiomatic WebSocket library for Go.

This library is in heavy development.

## Install

```bash
go get nhooyr.io/ws
```

## Why

There is no other Go WebSocket library with a clean API.

Comparisons with existing WebSocket libraries below.

### [x/net/websocket](https://godoc.org/golang.org/x/net/websocket)


Unmaintained and the API does not reflect WebSocket semantics.

See https://github.com/golang/go/issues/18152

### [gorilla/websocket](https://github.com/gorilla/websocket)

This package is the community standard but it is very old and over time
has accumulated cruft. There are many ways to do the same thing and the API
overall is just not very clear. 

The callback hooks are also confusing. The API for this library has been designed
such that there is only one way to do things and callbacks have been avoided.

Performance sensitive applications should use ws/wscore directly.

### [gobwas/ws](https://github.com/gobwas/ws)

This library has an extremely flexible API but that comes at a cost of usability
and clarity. Its just not clear and simple how to do things in a safe manner. 
