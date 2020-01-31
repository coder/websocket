// +build !js

package websocket_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"cdr.dev/slog/sloggers/slogtest/assert"
	"golang.org/x/xerrors"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/errd"
)

var excludedAutobahnCases = []string{
	// We skip the UTF-8 handling tests as there isn't any reason to reject invalid UTF-8, just
	// more performance overhead.
	"6.*", "7.5.1",

	// We skip the tests related to requestMaxWindowBits as that is unimplemented due
	// to limitations in compress/flate. See https://github.com/golang/go/issues/3155
	"13.3.*", "13.4.*", "13.5.*", "13.6.*",

	"12.*",
	"13.*",
}

var autobahnCases = []string{"*"}

// https://github.com/crossbario/autobahn-python/tree/master/wstest
func TestAutobahn(t *testing.T) {
	t.Parallel()

	if os.Getenv("AUTOBAHN") == "" {
		t.Skip("Set $AUTOBAHN to run tests against the autobahn test suite")
	}

	t.Run("server", testServerAutobahn)
	t.Run("client", testClientAutobahn)
}

func testServerAutobahn(t *testing.T) {
	t.Parallel()

	s, closeFn := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols: []string{"echo"},
		})
		assert.Success(t, "accept", err)
		err = echoLoop(r.Context(), c)
		assertCloseStatus(t, websocket.StatusNormalClosure, err)
	}, false)
	defer closeFn()

	specFile, err := tempJSONFile(map[string]interface{}{
		"outdir": "ci/out/wstestServerReports",
		"servers": []interface{}{
			map[string]interface{}{
				"agent": "main",
				"url":   strings.Replace(s.URL, "http", "ws", 1),
			},
		},
		"cases":         autobahnCases,
		"exclude-cases": excludedAutobahnCases,
	})
	assert.Success(t, "tempJSONFile", err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*10)
	defer cancel()

	args := []string{"--mode", "fuzzingclient", "--spec", specFile}
	wstest := exec.CommandContext(ctx, "wstest", args...)
	_, err = wstest.CombinedOutput()
	assert.Success(t, "wstest", err)

	checkWSTestIndex(t, "./ci/out/wstestServerReports/index.json")
}

func testClientAutobahn(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	wstestURL, closeFn, err := wstestClientServer(ctx)
	assert.Success(t, "wstestClient", err)
	defer closeFn()

	err = waitWS(ctx, wstestURL)
	assert.Success(t, "waitWS", err)

	cases, err := wstestCaseCount(ctx, wstestURL)
	assert.Success(t, "wstestCaseCount", err)

	t.Run("cases", func(t *testing.T) {
		// Max 8 cases running at a time.
		mu := make(chan struct{}, 8)

		for i := 1; i <= cases; i++ {
			i := i
			t.Run("", func(t *testing.T) {
				t.Parallel()

				mu <- struct{}{}
				defer func() {
					<-mu
				}()

				ctx, cancel := context.WithTimeout(ctx, time.Second*45)
				defer cancel()

				c, _, err := websocket.Dial(ctx, fmt.Sprintf(wstestURL+"/runCase?case=%v&agent=main", i), nil)
				assert.Success(t, "autobahn dial", err)

				err = echoLoop(ctx, c)
				t.Logf("echoLoop: %+v", err)
			})
		}
	})

	c, _, err := websocket.Dial(ctx, fmt.Sprintf(wstestURL+"/updateReports?agent=main"), nil)
	assert.Success(t, "dial", err)
	c.Close(websocket.StatusNormalClosure, "")

	checkWSTestIndex(t, "./ci/out/wstestClientReports/index.json")
}

func waitWS(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()

	for ctx.Err() == nil {
		c, _, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			continue
		}
		c.Close(websocket.StatusNormalClosure, "")
		return nil
	}

	return ctx.Err()
}

func wstestClientServer(ctx context.Context) (url string, closeFn func(), err error) {
	serverAddr, err := unusedListenAddr()
	if err != nil {
		return "", nil, err
	}

	url = "ws://" + serverAddr

	specFile, err := tempJSONFile(map[string]interface{}{
		"url":           url,
		"outdir":        "ci/out/wstestClientReports",
		"cases":         autobahnCases,
		"exclude-cases": excludedAutobahnCases,
	})
	if err != nil {
		return "", nil, xerrors.Errorf("failed to write spec: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer func() {
		if err != nil {
			cancel()
		}
	}()

	args := []string{"--mode", "fuzzingserver", "--spec", specFile,
		// Disables some server that runs as part of fuzzingserver mode.
		// See https://github.com/crossbario/autobahn-testsuite/blob/058db3a36b7c3a1edf68c282307c6b899ca4857f/autobahntestsuite/autobahntestsuite/wstest.py#L124
		"--webport=0",
	}
	wstest := exec.CommandContext(ctx, "wstest", args...)
	err = wstest.Start()
	if err != nil {
		return "", nil, xerrors.Errorf("failed to start wstest: %w", err)
	}

	return url, func() {
		wstest.Process.Kill()
	}, nil
}

func wstestCaseCount(ctx context.Context, url string) (cases int, err error) {
	defer errd.Wrap(&err, "failed to get case count")

	c, _, err := websocket.Dial(ctx, url+"/getCaseCount", nil)
	if err != nil {
		return 0, err
	}
	defer c.Close(websocket.StatusInternalError, "")

	_, r, err := c.Reader(ctx)
	if err != nil {
		return 0, err
	}
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return 0, err
	}
	cases, err = strconv.Atoi(string(b))
	if err != nil {
		return 0, err
	}

	c.Close(websocket.StatusNormalClosure, "")

	return cases, nil
}

func checkWSTestIndex(t *testing.T, path string) {
	wstestOut, err := ioutil.ReadFile(path)
	assert.Success(t, "ioutil.ReadFile", err)

	var indexJSON map[string]map[string]struct {
		Behavior      string `json:"behavior"`
		BehaviorClose string `json:"behaviorClose"`
	}
	err = json.Unmarshal(wstestOut, &indexJSON)
	assert.Success(t, "json.Unmarshal", err)

	for _, tests := range indexJSON {
		for test, result := range tests {
			t.Run(test, func(t *testing.T) {
				switch result.BehaviorClose {
				case "OK", "INFORMATIONAL":
				default:
					t.Errorf("bad close behaviour")
				}

				switch result.Behavior {
				case "OK", "NON-STRICT", "INFORMATIONAL":
				default:
					t.Errorf("failed")
				}
			})
		}
	}

	if t.Failed() {
		htmlPath := strings.Replace(path, ".json", ".html", 1)
		t.Errorf("detected autobahn violation, see %q", htmlPath)
	}
}

func unusedListenAddr() (_ string, err error) {
	defer errd.Wrap(&err, "failed to get unused listen address")
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return "", err
	}
	l.Close()
	return l.Addr().String(), nil
}

func tempJSONFile(v interface{}) (string, error) {
	f, err := ioutil.TempFile("", "temp.json")
	if err != nil {
		return "", xerrors.Errorf("temp file: %w", err)
	}
	defer f.Close()

	e := json.NewEncoder(f)
	e.SetIndent("", "\t")
	err = e.Encode(v)
	if err != nil {
		return "", xerrors.Errorf("json encode: %w", err)
	}

	err = f.Close()
	if err != nil {
		return "", xerrors.Errorf("close temp file: %w", err)
	}

	return f.Name(), nil
}
