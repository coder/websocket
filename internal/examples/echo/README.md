# Echo Example

This directory contains a echo server example using nhooyr.io/websocket.

```bash
$ cd examples/echo
$ go run . localhost:0
listening on ws://127.0.0.1:51055
```

You can use a WebSocket client like https://github.com/hashrocket/ws to connect. All messages
written will be echoed back.

## Structure

The server is in `server.go` and is implemented as a `http.HandlerFunc` that accepts the WebSocket
and then reads all messages and writes them exactly as is back to the connection.

`server_test.go` contains a small unit test to verify it works correctly.

`main.go` brings it all together so that you can run it and play around with it.

## Detailed Guide

### In main.go

1. Project starts. `func main` as the entry point start the program.

2. main func execute `run()` and `log.Fatal` is there is `err`

3. `run()` checks if `args` length is < 2 returns `err` if not
   if not it start a `"tcp"` connection and assign `l` as `net.Listener` with `err`,
   if there is `err` `run()` will stop and return if not it will `log.Printf("listening on ws:%v", l.Addr())`

4. then `run()` will make a `s:= &http.Server{ Handler: echoServer struct{ logf: log.Printf, ... } with Read, WriteTimeout: time... },`

5. then `run()` will create a buffered channel `errc := (chan error, 1)`
   after then it make a anonymous `go func with s := &htttp.Server s's s.Serve(l)` - `Serve()` method
   passing `l net.Listener` as func argument, note that this `go func` will run in a seperate go routine, not in main go routine.

6. then `run()` create another buffered channel as `sigs:= make(chan os.Signal, 1)`
   for when user terminate the program by pressing <kbd>Ctrl + c</kbd> in terminal or stop manually by something else...

7. then `run()` will check for both buffered channel `errc` and `sigs` case and `log.Printf(...)` based on those cases.

8. then `run()` create a `ctx, cancel := context.WithTimeout` with sepecified time and passes ctx in `s's Shutdown()` method.

---

> **Note:** `s.Serve(l)` is running in another go routine
>
> - Also, when you start the program using `$ go run . localhost:0` (0 is placeholder you will specify your own port.)
> - you run both `main.go` and `server.go` both not only just `main.go`, so `server.go` also starts...

---

### In server.go

1. there is `echoServer struct` which was used in
   `run()`'s `s:= &http.Server{ Handler: echoServer struct{ logf: log.Printf, ... } with Read, WriteTimeout: time... },`

2. `func (s echoServer) ServeHTTP(w,r)` method starts and creates two variables `c, err := websocket.Accepts(w, r, &websocket.AcceptsOptions{....})`
   it check if `Subprotocols: is "echo"` or if there is `err`, if `err` occur, `Accept` send `err` and log it with `s.logf(err)` and `return` from `ServerHTTP`

3. also as specified in next line `defer c.CloseNow()` close the websocket connection.

4. if `Subprotocols:` is not `"echo"`, then it calls `c.Close()` and close handshake gracefully and return with given error message.

5. if `Subprotocols:` is `"echo"`, it start a `for loop` for the websocket connection and starts `err = echo(r.Context(), c, l)`.

- in `echo()` arguments `r.Context()` is for context, `c` is for that paricular websocket connection and `l` in this for `rate.Limiter` not `net.Listener`

- `for loop` then check for 2 `err cases`, 1st `err` check is for if websocket client has closed connection by themself.
- 2nd `err` check is for other potential `err` as usual for generic `err != nil {with s.logf... and all}`.
  in both `err` cases `func` `return` after `err`

6. in `echo(ctx context.Context, c *websocket.Conn, l *rate.Limiter)` `func` will send `error` as said above.

7. this `echo` `func` is very simple and straightforward it checks for the message and read it's type and then write it back using the `c.Writer`.
