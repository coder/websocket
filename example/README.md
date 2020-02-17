# Chat Example

This directory contains a full stack example
of a simple chat webapp using nhooyr.io/websocket.

```bash
$ cd example
$ go run . localhost:0
listening on http://127.0.0.1:51055
```

Visit the printed URL to submit and view broadcasted messages in a browser.

![Image of Example](https://i.imgur.com/iSdpZFT.png)

## Structure

The frontend is contained in `index.html`, `index.js` and `index.css`. It setups the
DOM with a form at the buttom to submit messages and at the top is a scrollable div
that is populated with new messages as they are broadcast. The messages are received
via a WebSocket and messages are published via a POST HTTP endpoint.

The server portion is `main.go` and `chat.go` and implements serving the static frontend
assets as well as the `/subscribe` WebSocket endpoint for subscribing to
broadcast messages and `/publish` for publishing messages.
