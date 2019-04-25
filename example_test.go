package websocket_test

import (
	"context"
	"log"
	"net/http"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

func ExampleAccept() {
	fn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, websocket.AcceptOptions{})
		if err != nil {
			log.Println(err)
			return
		}
		defer c.Close(websocket.StatusInternalError, "the sky is falling")

		ctx, cancel := context.WithTimeout(r.Context(), time.Second*10)
		defer cancel()

		var v interface{}
		err = wsjson.Read(ctx, c, &v)
		if err != nil {
			log.Println(err)
			return
		}

		log.Printf("received: %v", v)

		c.Close(websocket.StatusNormalClosure, "success")
	})

	http.ListenAndServe("localhost:8080", fn)
}

func ExampleDial() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c, _, err := websocket.Dial(ctx, "ws://localhost:8080", websocket.DialOptions{})
	if err != nil {
		log.Println(err)
		return
	}
	defer c.Close(websocket.StatusInternalError, "the sky is falling")

	err = wsjson.Write(ctx, c, "hi")
	if err != nil {
		log.Println(err)
		return
	}

	c.Close(websocket.StatusNormalClosure, "done")
}
