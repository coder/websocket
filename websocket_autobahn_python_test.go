// This file contains the old autobahn test suite tests that use the
// python binary. The approach is very clunky and slow so new tests
// have been written in pure Go in websocket_test.go.
// +build autobahn-python

package websocket_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// https://github.com/crossbario/autobahn-python/tree/master/wstest
func TestPythonAutobahnServer(t *testing.T) {
	t.Parallel()

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := Accept(w, r, &AcceptOptions{
			Subprotocols: []string{"echo"},
		})
		if err != nil {
			t.Logf("server handshake failed: %+v", err)
			return
		}
		echoLoop(r.Context(), c)
	}))
	defer s.Close()

	spec := map[string]interface{}{
		"outdir": "ci/out/wstestServerReports",
		"servers": []interface{}{
			map[string]interface{}{
				"agent": "main",
				"url":   strings.Replace(s.URL, "http", "ws", 1),
			},
		},
		"cases": []string{"*"},
		// We skip the UTF-8 handling tests as there isn't any reason to reject invalid UTF-8, just
		// more performance overhead. 7.5.1 is the same.
		// 12.* and 13.* as we do not support compression.
		"exclude-cases": []string{"6.*", "7.5.1", "12.*", "13.*"},
	}
	specFile, err := ioutil.TempFile("", "websocketFuzzingClient.json")
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
	wstest := exec.CommandContext(ctx, "wstest", args...)
	out, err := wstest.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run wstest: %v\nout:\n%s", err, out)
	}

	checkWSTestIndex(t, "./ci/out/wstestServerReports/index.json")
}

func unusedListenAddr() (string, error) {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return "", err
	}
	l.Close()
	return l.Addr().String(), nil
}

// https://github.com/crossbario/autobahn-python/blob/master/wstest/testee_client_aio.py
func TestPythonAutobahnClientOld(t *testing.T) {
	t.Parallel()

	serverAddr, err := unusedListenAddr()
	if err != nil {
		t.Fatalf("failed to get unused listen addr for wstest: %v", err)
	}

	wsServerURL := "ws://" + serverAddr

	spec := map[string]interface{}{
		"url":    wsServerURL,
		"outdir": "ci/out/wstestClientReports",
		"cases":  []string{"*"},
		// See TestAutobahnServer for the reasons why we exclude these.
		"exclude-cases": []string{"6.*", "7.5.1", "12.*", "13.*"},
	}
	specFile, err := ioutil.TempFile("", "websocketFuzzingServer.json")
	if err != nil {
		t.Fatalf("failed to create temp file for fuzzingserver.json: %v", err)
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

	args := []string{"--mode", "fuzzingserver", "--spec", specFile.Name(),
		// Disables some server that runs as part of fuzzingserver mode.
		// See https://github.com/crossbario/autobahn-testsuite/blob/058db3a36b7c3a1edf68c282307c6b899ca4857f/autobahntestsuite/autobahntestsuite/wstest.py#L124
		"--webport=0",
	}
	wstest := exec.CommandContext(ctx, "wstest", args...)
	err = wstest.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := wstest.Process.Kill()
		if err != nil {
			t.Error(err)
		}
	}()

	// Let it come up.
	time.Sleep(time.Second * 5)

	var cases int
	func() {
		c, _, err := Dial(ctx, wsServerURL+"/getCaseCount", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close(StatusInternalError, "")

		_, r, err := c.Reader(ctx)
		if err != nil {
			t.Fatal(err)
		}
		b, err := ioutil.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		cases, err = strconv.Atoi(string(b))
		if err != nil {
			t.Fatal(err)
		}

		c.Close(StatusNormalClosure, "")
	}()

	for i := 1; i <= cases; i++ {
		func() {
			ctx, cancel := context.WithTimeout(ctx, time.Second*45)
			defer cancel()

			c, _, err := Dial(ctx, fmt.Sprintf(wsServerURL+"/runCase?case=%v&agent=main", i), nil)
			if err != nil {
				t.Fatal(err)
			}
			echoLoop(ctx, c)
		}()
	}

	c, _, err := Dial(ctx, fmt.Sprintf(wsServerURL+"/updateReports?agent=main"), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Close(StatusNormalClosure, "")

	checkWSTestIndex(t, "./ci/out/wstestClientReports/index.json")
}

func checkWSTestIndex(t *testing.T, path string) {
	wstestOut, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read index.json: %v", err)
	}

	var indexJSON map[string]map[string]struct {
		Behavior      string `json:"behavior"`
		BehaviorClose string `json:"behaviorClose"`
	}
	err = json.Unmarshal(wstestOut, &indexJSON)
	if err != nil {
		t.Fatalf("failed to unmarshal index.json: %v", err)
	}

	var failed bool
	for _, tests := range indexJSON {
		for test, result := range tests {
			switch result.Behavior {
			case "OK", "NON-STRICT", "INFORMATIONAL":
			default:
				failed = true
				t.Errorf("test %v failed", test)
			}
			switch result.BehaviorClose {
			case "OK", "INFORMATIONAL":
			default:
				failed = true
				t.Errorf("bad close behaviour for test %v", test)
			}
		}
	}

	if failed {
		path = strings.Replace(path, ".json", ".html", 1)
		if os.Getenv("CI") == "" {
			t.Errorf("wstest found failure, see %q (output as an artifact in CI)", path)
		}
	}
}
