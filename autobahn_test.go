//go:build !js
// +build !js

package websocket_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/internal/errd"
	"nhooyr.io/websocket/internal/test/assert"
	"nhooyr.io/websocket/internal/test/wstest"
	"nhooyr.io/websocket/internal/util"
)

var excludedAutobahnCases = []string{
	// We skip the UTF-8 handling tests as there isn't any reason to reject invalid UTF-8, just
	// more performance overhead.
	"6.*", "7.5.1",

	// We skip the tests related to requestMaxWindowBits as that is unimplemented due
	// to limitations in compress/flate. See https://github.com/golang/go/issues/3155
	"13.3.*", "13.4.*", "13.5.*", "13.6.*",
}

var autobahnCases = []string{"*"}

// Used to run individual test cases. autobahnCases runs only those cases matched
// and not excluded by excludedAutobahnCases. Adding cases here means excludedAutobahnCases
// is niled.
var onlyAutobahnCases = []string{}

func TestAutobahn(t *testing.T) {
	t.Parallel()

	if os.Getenv("AUTOBAHN") == "" {
		t.SkipNow()
	}

	if os.Getenv("AUTOBAHN") == "fast" {
		// These are the slow tests.
		excludedAutobahnCases = append(excludedAutobahnCases,
			"9.*", "12.*", "13.*",
		)
	}

	if len(onlyAutobahnCases) > 0 {
		excludedAutobahnCases = []string{}
		autobahnCases = onlyAutobahnCases
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	wstestURL, closeFn, err := wstestServer(t, ctx)
	assert.Success(t, err)
	defer func() {
		assert.Success(t, closeFn())
	}()

	err = waitWS(ctx, wstestURL)
	assert.Success(t, err)

	cases, err := wstestCaseCount(ctx, wstestURL)
	assert.Success(t, err)

	t.Run("cases", func(t *testing.T) {
		for i := 1; i <= cases; i++ {
			i := i
			t.Run("", func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
				defer cancel()

				c, _, err := websocket.Dial(ctx, fmt.Sprintf(wstestURL+"/runCase?case=%v&agent=main", i), &websocket.DialOptions{
					CompressionMode: websocket.CompressionContextTakeover,
				})
				assert.Success(t, err)
				err = wstest.EchoLoop(ctx, c)
				t.Logf("echoLoop: %v", err)
			})
		}
	})

	c, _, err := websocket.Dial(ctx, fmt.Sprintf(wstestURL+"/updateReports?agent=main"), nil)
	assert.Success(t, err)
	c.Close(websocket.StatusNormalClosure, "")

	checkWSTestIndex(t, "./ci/out/autobahn-report/index.json")
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

func wstestServer(tb testing.TB, ctx context.Context) (url string, closeFn func() error, err error) {
	defer errd.Wrap(&err, "failed to start autobahn wstest server")

	serverAddr, err := unusedListenAddr()
	if err != nil {
		return "", nil, err
	}
	_, serverPort, err := net.SplitHostPort(serverAddr)
	if err != nil {
		return "", nil, err
	}

	url = "ws://" + serverAddr
	const outDir = "ci/out/autobahn-report"

	specFile, err := tempJSONFile(map[string]interface{}{
		"url":           url,
		"outdir":        outDir,
		"cases":         autobahnCases,
		"exclude-cases": excludedAutobahnCases,
	})
	if err != nil {
		return "", nil, fmt.Errorf("failed to write spec: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Hour)
	defer func() {
		if err != nil {
			cancel()
		}
	}()

	dockerPull := exec.CommandContext(ctx, "docker", "pull", "crossbario/autobahn-testsuite")
	dockerPull.Stdout = util.WriterFunc(func(p []byte) (int, error) {
		tb.Log(string(p))
		return len(p), nil
	})
	dockerPull.Stderr = util.WriterFunc(func(p []byte) (int, error) {
		tb.Log(string(p))
		return len(p), nil
	})
	tb.Log(dockerPull)
	err = dockerPull.Run()
	if err != nil {
		return "", nil, fmt.Errorf("failed to pull docker image: %w", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", nil, err
	}

	var args []string
	args = append(args, "run", "-i", "--rm",
		"-v", fmt.Sprintf("%s:%[1]s", specFile),
		"-v", fmt.Sprintf("%s/ci:/ci", wd),
		fmt.Sprintf("-p=%s:%s", serverAddr, serverPort),
		"crossbario/autobahn-testsuite",
	)
	args = append(args, "wstest", "--mode", "fuzzingserver", "--spec", specFile,
		// Disables some server that runs as part of fuzzingserver mode.
		// See https://github.com/crossbario/autobahn-testsuite/blob/058db3a36b7c3a1edf68c282307c6b899ca4857f/autobahntestsuite/autobahntestsuite/wstest.py#L124
		"--webport=0",
	)
	wstest := exec.CommandContext(ctx, "docker", args...)
	wstest.Stdout = util.WriterFunc(func(p []byte) (int, error) {
		tb.Log(string(p))
		return len(p), nil
	})
	wstest.Stderr = util.WriterFunc(func(p []byte) (int, error) {
		tb.Log(string(p))
		return len(p), nil
	})
	tb.Log(wstest)
	err = wstest.Start()
	if err != nil {
		return "", nil, fmt.Errorf("failed to start wstest: %w", err)
	}

	return url, func() error {
		err = wstest.Process.Kill()
		if err != nil {
			return fmt.Errorf("failed to kill wstest: %w", err)
		}
		err = wstest.Wait()
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == -1 {
			return nil
		}
		return err
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
	b, err := io.ReadAll(r)
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
	wstestOut, err := os.ReadFile(path)
	assert.Success(t, err)

	var indexJSON map[string]map[string]struct {
		Behavior      string `json:"behavior"`
		BehaviorClose string `json:"behaviorClose"`
	}
	err = json.Unmarshal(wstestOut, &indexJSON)
	assert.Success(t, err)

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
	f, err := os.CreateTemp("", "temp.json")
	if err != nil {
		return "", fmt.Errorf("temp file: %w", err)
	}
	defer f.Close()

	e := json.NewEncoder(f)
	e.SetIndent("", "\t")
	err = e.Encode(v)
	if err != nil {
		return "", fmt.Errorf("json encode: %w", err)
	}

	err = f.Close()
	if err != nil {
		return "", fmt.Errorf("close temp file: %w", err)
	}

	return f.Name(), nil
}
