# Chat Example

This directory contains a full stack example of a simple chat webapp using nhooyr.io/websocket.

```bash
$ cd example
$ go run . localhost:0
listening on http://127.0.0.1:51055
```

Visit the printed URL to submit and view broadcasted messages in a browser.

![Image of Example](https://i.imgur.com/iSdpZFT.png)

## Structure

The frontend is contained in `index.html`, `index.js` and `index.css`. It sets up the
DOM with a scrollable div at the top that is populated with new messages as they are broadcast.
At the bottom it adds a form to submit messages.
The messages are received via the WebSocket `/subscribe` endpoint and published via
the HTTP POST `/publish` endpoint.

The server portion is `main.go` and `chat.go` and implements serving the static frontend
assets, the `/subscribe` WebSocket endpoint and the HTTP POST `/publish` endpoint.
