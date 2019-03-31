package websocket_test

import (
	"context"
	"encoding/json"
	"golang.org/x/time/rate"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"nhooyr.io/websocket"
)

var httpclient = &http.Client{
	Timeout: time.Second * 15,
}

func TestConnection(t *testing.T) {
	t.Parallel()

	obj := make(chan interface{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r,
			websocket.AcceptSubprotocols("myproto"),
		)
		if err != nil {
			t.Errorf("failed to accept connection: %v", err)
			return
		}
		defer c.Close(websocket.StatusInternalError, "")

		ctx, cancel := context.WithTimeout(r.Context(), time.Second*5)
		defer cancel()

		var v interface{}
		err = websocket.ReadJSON(ctx, c, &v)
		if err != nil {
			t.Error(err)
			return
		}

		obj <- v

		c.Close(websocket.StatusNormalClosure, "")
	}))
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, s.URL,
		websocket.DialSubprotocols("myproto"),
	)
	if err != nil {
		t.Fatalf("failed to do handshake request: %v", err)
	}
	defer c.Close(websocket.StatusInternalError, "")

	checkHeader := func(h, exp string) {
		t.Helper()
		value := resp.Header.Get(h)
		if exp != value {
			t.Errorf("expected different value for header %v: %v", h, cmp.Diff(exp, value))
		}
	}

	checkHeader("Connection", "Upgrade")
	checkHeader("Upgrade", "websocket")
	checkHeader("Sec-WebSocket-Accept", "ICX+Yqv66kxgM0FcWaLWlFLwTAI=")
	checkHeader("Sec-WebSocket-Protocol", "myproto")

	v := map[string]interface{}{
		"anmol": "wowow",
	}
	err = websocket.WriteJSON(ctx, c, v)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case v2 := <-obj:
		if !cmp.Equal(v, v2) {
			t.Fatalf("unexpected value read: %v", cmp.Diff(v, v2))
		}
	case <-time.After(time.Second * 10):
		t.Fatalf("test timed out")
	}
}

func TestAutobahn(t *testing.T) {
	t.Parallel()

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r,
			websocket.AcceptSubprotocols("echo"),
		)
		if err != nil {
			t.Logf("server handshake failed: %v", err)
			return
		}
		defer c.Close(websocket.StatusInternalError, "")

		ctx := context.Background()

		echo := func() error {
			ctx, cancel := context.WithTimeout(ctx, time.Minute)
			defer cancel()

			typ, r, err := c.ReadMessage(ctx)
			if err != nil {
				return err
			}

			ctx, cancel = context.WithTimeout(ctx, time.Second*10)
			defer cancel()

			r.SetContext(ctx)
			r.Limit(131072)

			w := c.MessageWriter(typ)
			w.SetContext(ctx)
			_, err = io.Copy(w, r)
			if err != nil {
				return err
			}

			err = w.Close()
			if err != nil {
				return err
			}

			return nil
		}

		l := rate.NewLimiter(rate.Every(time.Millisecond*100), 10)
		for l.Allow() {
			err := echo()
			if err != nil {
				t.Logf("%v: failed to echo message: %+v", time.Now(), err)
				return
			}
		}
	}))
	defer s.Close()

	spec := map[string]interface{}{
		"outdir": "wstest_reports/server",
		"servers": []interface{}{
			map[string]interface{}{
				"agent": "main",
				"url":   strings.Replace(s.URL, "http", "ws", 1),
				"options": map[string]interface{}{
					"version": 18,
				},
			},
		},
		"cases":         []string{"*"},
		"exclude-cases": []interface{}{},
	}
	specFile, err := ioutil.TempFile("", "websocket_fuzzingclient.json")
	if err != nil {
		t.Fatalf("failed to create temp file for fuzzingclient.json: %v", err)
	}
	defer specFile.Close()

	e := json.NewEncoder(specFile)
	e.SetIndent("", "\t")
	err = e.Encode(spec)
	if err != nil {
		t.Fatalf("failed to write spec: %v", err)
	}

	err = specFile.Close()
	if err != nil {
		t.Fatalf("failed to close file: %v", err)
	}

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Minute*10)
	defer cancel()

	args := []string{"--mode", "fuzzingclient", "--spec", specFile.Name()}
	if os.Getenv("CI") == "" {
		args = append([]string{"--debug"}, args...)
	}
	wstest := exec.CommandContext(ctx, "wstest", args...)
	out, err := wstest.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run wstest: %v\nout:\n%s", err, out)
	}

	b, err := ioutil.ReadFile("./wstest_reports/server/index.json")
	if err != nil {
		t.Fatalf("failed to read index.json: %v", err)
	}

	if testing.Verbose() {
		t.Logf("output: %s", out)
	}

	var indexJSON map[string]map[string]struct {
		Behavior string `json:"behavior"`
	}
	err = json.Unmarshal(b, &indexJSON)
	if err != nil {
		t.Fatalf("failed to unmarshal index.json: %v", err)
	}

	var failed bool
	for _, tests := range indexJSON {
		for test, result := range tests {
			if result.Behavior != "OK" {
				failed = true
				t.Errorf("test %v failed", test)
			}
		}
	}

	if failed {
		if os.Getenv("CI") == "" {
			t.Errorf("wstest found failure, please see ./wstest_reports/server/index.html")
		} else {
			t.Errorf("wstest found failure, please run test.sh locally to see ./wstest_reports/server/index.html")
		}
	}
}
