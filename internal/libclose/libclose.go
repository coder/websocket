// Package libclose lets other packages in this module close a websocket.Conn
// the way package websocket closes it itself, so the close does not count as
// the application's first Close call.
package libclose

// Close holds (*websocket.Conn).closeWithHandshake and has the type
// func(*websocket.Conn, websocket.StatusCode, string) error. Package websocket
// sets it during initialization. It has type any because package websocket
// imports this package, so this package cannot name those types.
var Close any
