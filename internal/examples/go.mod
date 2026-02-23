module github.com/coder/websocket/examples

go 1.24.7

replace github.com/coder/websocket => ../..

require (
	github.com/coder/websocket v0.0.0-00010101000000-000000000000
	golang.org/x/net v0.44.0
	golang.org/x/time v0.13.0
)

require golang.org/x/text v0.29.0 // indirect
