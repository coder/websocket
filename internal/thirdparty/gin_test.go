package thirdparty

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/errd"
	"nhooyr.io/websocket/internal/test/assert"
	"nhooyr.io/websocket/internal/test/wstest"
	"nhooyr.io/websocket/wsjson"
)

func TestGin(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/", func(ginCtx *gin.Context) {
		err := echoServer(ginCtx.Writer, ginCtx.Request, nil)
		if err != nil {
			t.Error(err)
		}
	})

	s := httptest.NewServer(r)
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	c, _, err := websocket.Dial(ctx, s.URL, nil)
	assert.Success(t, err)
	defer c.Close(websocket.StatusInternalError, "")

	err = wsjson.Write(ctx, c, "hello")
	assert.Success(t, err)

	var v interface{}
	err = wsjson.Read(ctx, c, &v)
	assert.Success(t, err)
	assert.Equal(t, "read msg", "hello", v)

	err = c.Close(websocket.StatusNormalClosure, "")
	assert.Success(t, err)
}

func echoServer(w http.ResponseWriter, r *http.Request, opts *websocket.AcceptOptions) (err error) {
	defer errd.Wrap(&err, "echo server failed")

	c, err := websocket.Accept(w, r, opts)
	if err != nil {
		return err
	}
	defer c.Close(websocket.StatusInternalError, "")

	err = wstest.EchoLoop(r.Context(), c)
	return assertCloseStatus(websocket.StatusNormalClosure, err)
}

func assertCloseStatus(exp websocket.StatusCode, err error) error {
	if websocket.CloseStatus(err) == -1 {
		return fmt.Errorf("expected websocket.CloseError: %T %v", err, err)
	}
	if websocket.CloseStatus(err) != exp {
		return fmt.Errorf("expected close status %v but got %v", exp, err)
	}
	return nil
}
