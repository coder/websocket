package http2

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/coder/websocket"
	"github.com/coder/websocket/internal/test/assert"
	"github.com/coder/websocket/internal/test/wstest"
)

// withGODEBUG re-execs the current test function with the desired http2xconnect
// setting. This is necessary because x/net/http2 reads GODEBUG at init time and
// does not notice changes made via t.Setenv. No re-execution is needed if the
// desired setting is already enabled.
func withGODEBUG(t *testing.T, wantOn bool) (hasEnv bool) {
	t.Helper()
	const guardEnv = "WS_RUN_HTTP2_CHILD"
	if os.Getenv(guardEnv) == t.Name() {
		return true
	}
	// Build desired GODEBUG.
	var enabled bool
	var filtered []string
	for kv := range strings.SplitSeq(os.Getenv("GODEBUG"), ",") {
		if v, ok := strings.CutPrefix(kv, "http2xconnect="); ok {
			enabled = v == "1"
			continue
		}
		filtered = append(filtered, kv)
	}
	if wantOn {
		if enabled {
			return true
		}
		filtered = append(filtered, "http2xconnect=1")
	} else {
		if !enabled {
			return true
		}
		filtered = append(filtered, "http2xconnect=0")
	}
	newGODEBUG := strings.Join(filtered, ",")

	// Re-exec this test function only.
	cmd := exec.Command(os.Args[0], append(slices.Clone(os.Args[1:]), []string{"-test.run", "^" + regexp.QuoteMeta(t.Name()) + "$"}...)...)
	cmd.Env = append(os.Environ(),
		"GODEBUG="+newGODEBUG,
		guardEnv+"="+t.Name(),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess failed for %s (GODEBUG=%s): error: %v\n%s", t.Name(), newGODEBUG, err, out)
	} else {
		t.Logf("subprocess succeeded for %s (GODEBUG=%s):\n%s", t.Name(), newGODEBUG, out)
	}
	return false
}

// newH2TLSClient returns an *http.Client that always uses http2.Transport over
// TLS (from golang.org/x/net). For test simplicity, it disables verification.
func newH2TLSClient() *http.Client {
	h2t := &http2.Transport{
		TLSClientConfig: &tls.Config{
			NextProtos:         []string{http2.NextProtoTLS},
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true,
		},
		MaxReadFrameSize: 1024 * 1024,
	}
	return &http.Client{Transport: h2t}
}

type customRoundTripper struct {
	http.RoundTripper
	pre  func(*http.Request)
	post func(*http.Response)
}

func (t *customRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.pre != nil {
		t.pre(req)
	}
	resp, err := t.RoundTripper.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if t.post != nil {
		t.post(resp)
	}
	return resp, nil
}

// newH2TLSClientCustomRoundTripper is like newH2TLSClient but allows pre and
// post processing of requests and responses.
func newH2TLSClientCustomRoundTripper(pre func(*http.Request), post func(*http.Response)) *http.Client {
	h2t := &http2.Transport{
		TLSClientConfig: &tls.Config{
			NextProtos:         []string{http2.NextProtoTLS},
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true,
		},
		MaxReadFrameSize: 1024 * 1024,
	}
	return &http.Client{Transport: &customRoundTripper{RoundTripper: h2t, pre: pre, post: post}}
}

// newH2CClient returns an *http.Client that uses an http2.Transport configured
// for cleartext (h2c). (Server support for h2c is required as well.)
func newH2CClient() *http.Client {
	h2t := &http2.Transport{
		AllowHTTP: true,
		DialTLS: func(network, addr string, _ *tls.Config) (net.Conn, error) {
			return net.Dial(network, addr)
		},
	}
	return &http.Client{Transport: h2t}
}

// newH1TLSClient returns an *http.Client that forces HTTP/1.1. For test
// simplicity, it disables verification.
func newH1TLSClient() *http.Client {
	h1t := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true,
		},
		ForceAttemptHTTP2: false,
	}
	return &http.Client{Transport: h1t}
}

type runTableTestCase struct {
	name        string
	scheme      string // "wss" or "ws"
	client      func(*testing.T) *http.Client
	clientProto websocket.HTTPProtocol
	serverProto websocket.HTTPProtocol
	wantProto   int  // 1 or 2
	wantStatus  int  // Wanted status code (e.g., 200 or 100).
	wantErr     bool // Want a Dial error.
}

// runTable executes a table of cases under the current GODEBUG mode.
func runTable(t *testing.T, cases []runTableTestCase) {
	for _, tc := range cases {
		echoHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{HTTPProtocol: tc.serverProto})
			if err != nil {
				return
			}
			defer conn.CloseNow()
			_ = wstest.EchoLoop(r.Context(), conn)
		})

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Start server depending on scheme and serverMode.
			var srvURL string
			switch tc.scheme {
			case "wss":
				srv := httptest.NewUnstartedServer(echoHandler)
				srv.EnableHTTP2 = true
				_ = http2.ConfigureServer(srv.Config, &http2.Server{})
				srv.StartTLS()
				defer srv.Close()
				srvURL = strings.Replace(srv.URL, "https://", "wss://", 1)
			case "ws":
				srv := httptest.NewUnstartedServer(h2c.NewHandler(echoHandler, &http2.Server{}))
				srv.Start()
				defer srv.Close()
				srvURL = strings.Replace(srv.URL, "http://", "ws://", 1)
			default:
				t.Fatalf("unsupported scheme: %q", tc.scheme)
			}

			// Perform client connect.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			conn, resp, err := websocket.Dial(ctx, srvURL, &websocket.DialOptions{
				HTTPClient:   tc.client(t),
				HTTPProtocol: tc.clientProto,
			})
			if conn != nil {
				defer conn.CloseNow()
			}

			if err != nil && resp != nil {
				b, _ := io.ReadAll(resp.Body)
				t.Logf("Response body: %s", string(b))
			}
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			assert.Success(t, err)

			if tc.wantProto != 0 && (resp == nil || resp.ProtoMajor != tc.wantProto) {
				t.Fatalf("expected HTTP/%d response, got %v", tc.wantProto, resp)
			}
			if tc.wantStatus != 0 && (resp == nil || resp.StatusCode != tc.wantStatus) {
				t.Fatalf("expected status %d, got %v", tc.wantStatus, resp)
			}
			assert.Success(t, wstest.Echo(ctx, conn, 1<<10))
		})
	}
}

var sharedTestCases = []runTableTestCase{
	{
		name:        "Error TLS ClientHTTP1 RequestHTTP2 AcceptAny",
		scheme:      "wss",
		client:      func(t *testing.T) *http.Client { return newH1TLSClient() },
		clientProto: websocket.HTTPProtocol2,
		serverProto: websocket.HTTPProtocolAny,
		wantErr:     true,
	},
	{
		name:        "Error H2C ClientHTTP1 RequestHTTP2 AcceptAny",
		scheme:      "ws",
		client:      func(t *testing.T) *http.Client { return newH1TLSClient() },
		clientProto: websocket.HTTPProtocol2,
		serverProto: websocket.HTTPProtocolAny,
		wantErr:     true,
	},
	{
		name:        "Error TLS ClientHTTP2 RequestHTTP2 AcceptHTTP1",
		scheme:      "wss",
		client:      func(t *testing.T) *http.Client { return newH2TLSClient() },
		clientProto: websocket.HTTPProtocol2,
		serverProto: websocket.HTTPProtocol1,
		wantErr:     true,
	},
	{
		name:        "Error TLS ClientHTTP1 RequestHTTP1 AcceptHTTP2",
		scheme:      "wss",
		client:      func(t *testing.T) *http.Client { return newH1TLSClient() },
		clientProto: websocket.HTTPProtocol1,
		serverProto: websocket.HTTPProtocol2,
		wantErr:     true,
	},
}

func TestHTTP2Suite_XCONNECT_Enabled(t *testing.T) {
	if !withGODEBUG(t, true) {
		return
	}
	runTable(t, append(sharedTestCases, []runTableTestCase{
		{
			name:        "OK TLS ClientHTTP2 RequestHTTP2 AcceptHTTP2",
			scheme:      "wss",
			client:      func(t *testing.T) *http.Client { return newH2TLSClient() },
			clientProto: websocket.HTTPProtocol2,
			serverProto: websocket.HTTPProtocol2,
			wantProto:   2,
		},
		{
			name:        "OK TLS ClientHTTP2 RequestHTTP2 AcceptAny",
			scheme:      "wss",
			client:      func(t *testing.T) *http.Client { return newH2TLSClient() },
			clientProto: websocket.HTTPProtocol2,
			serverProto: websocket.HTTPProtocolAny,
			wantProto:   2,
		},
		{
			name:        "OK H2C ClientHTTP2 RequestHTTP2 AcceptAny",
			scheme:      "ws",
			client:      func(t *testing.T) *http.Client { return newH2CClient() },
			clientProto: websocket.HTTPProtocol2,
			serverProto: websocket.HTTPProtocolAny,
			wantProto:   2,
		},
		{
			name:   "Error server missing :protocol header",
			scheme: "wss",
			client: func(t *testing.T) *http.Client {
				return newH2TLSClientCustomRoundTripper(func(req *http.Request) {
					req.Header.Del(":protocol")
				}, nil)
			},
			clientProto: websocket.HTTPProtocol2,
			serverProto: websocket.HTTPProtocol2,
			wantErr:     true,
		},
		{
			// Note that this test actually fails inside the `http2` package and
			// does not reach our validation routine on the server side.
			//
			// 	http2: invalid :protocol header in non-CONNECT request
			name:   "Error server method not CONNECT",
			scheme: "wss",
			client: func(t *testing.T) *http.Client {
				return newH2TLSClientCustomRoundTripper(func(req *http.Request) {
					req.Method = http.MethodGet
				}, nil)
			},
			clientProto: websocket.HTTPProtocol2,
			serverProto: websocket.HTTPProtocol2,
			wantErr:     true,
		},
		{
			name:   "Error dial non-2xx response",
			scheme: "wss",
			client: func(t *testing.T) *http.Client {
				return newH2TLSClientCustomRoundTripper(nil, func(resp *http.Response) {
					resp.StatusCode = http.StatusSwitchingProtocols
					// Close body so we don't hang since this is actually a
					// CONNECT response.
					_ = resp.Body.Close()
				})
			},
			clientProto: websocket.HTTPProtocol2,
			serverProto: websocket.HTTPProtocol2,
			wantErr:     true,
		},
	}...))
}

func TestHTTP2Suite_XCONNECT_Disabled(t *testing.T) {
	if !withGODEBUG(t, false) {
		return
	}
	runTable(t, append(sharedTestCases, []runTableTestCase{
		{
			name:        "Error TLS ClientHTTP2 RequestHTTP2 AcceptAny NoExtendedConnect",
			scheme:      "wss",
			client:      func(t *testing.T) *http.Client { return newH2TLSClient() },
			clientProto: websocket.HTTPProtocol2,
			serverProto: websocket.HTTPProtocolAny,
			wantErr:     true,
		},
		{
			name:        "Error H2C ClientHTTP2 RequestHTTP2 AcceptAny NoExtendedConnect",
			scheme:      "ws",
			client:      func(t *testing.T) *http.Client { return newH2CClient() },
			clientProto: websocket.HTTPProtocol2,
			serverProto: websocket.HTTPProtocolAny,
			wantErr:     true,
		},
	}...))
}
