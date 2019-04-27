package websocket

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/xerrors"
)

// DialOptions represents the options available to pass to Dial.
type DialOptions struct {
	// HTTPClient is the http client used for the handshake.
	// Its Transport must use HTTP/1.1 and must return writable bodies
	// for WebSocket handshakes. This was introduced in Go 1.12.
	// http.Transport does this all correctly.
	HTTPClient *http.Client

	// HTTPHeader specifies the HTTP headers included in the handshake request.
	HTTPHeader http.Header

	// Subprotocols lists the subprotocols to negotiate with the server.
	Subprotocols []string
}

// We use this key for all client requests as the Sec-WebSocket-Key header is useless.
// See https://stackoverflow.com/a/37074398/4283659.
// We also use the same mask key for every message as it too does not make a difference.
var secWebSocketKey = base64.StdEncoding.EncodeToString(make([]byte, 16))

// Dial performs a WebSocket handshake on the given url with the given options.
// The response is the WebSocket handshake response from the server.
// If an error occurs, the returned response may be non nil. However, you can only
// read the first 1024 bytes of its body.
//
// This function requires at least Go 1.12 to succeed as it uses a new feature
// in net/http to perform WebSocket handshakes and get a writable body
// from the transport. See https://github.com/golang/go/issues/26937#issuecomment-415855861
func Dial(ctx context.Context, u string, opts DialOptions) (*Conn, *http.Response, error) {
	c, r, err := dial(ctx, u, opts)
	if err != nil {
		return nil, r, xerrors.Errorf("failed to websocket dial: %w", err)
	}
	return c, r, nil
}

func dial(ctx context.Context, u string, opts DialOptions) (_ *Conn, _ *http.Response, err error) {
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.HTTPClient.Timeout > 0 {
		return nil, nil, xerrors.Errorf("please use context for cancellation instead of http.Client.Timeout; see https://github.com/nhooyr/websocket/issues/67")
	}
	if opts.HTTPHeader == nil {
		opts.HTTPHeader = http.Header{}
	}

	parsedURL, err := url.Parse(u)
	if err != nil {
		return nil, nil, xerrors.Errorf("failed to parse url: %w", err)
	}

	switch parsedURL.Scheme {
	case "ws":
		parsedURL.Scheme = "http"
	case "wss":
		parsedURL.Scheme = "https"
	default:
		return nil, nil, xerrors.Errorf("unexpected url scheme: %q", parsedURL.Scheme)
	}

	req, _ := http.NewRequest("GET", parsedURL.String(), nil)
	req = req.WithContext(ctx)
	req.Header = opts.HTTPHeader
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", secWebSocketKey)
	if len(opts.Subprotocols) > 0 {
		req.Header.Set("Sec-WebSocket-Protocol", strings.Join(opts.Subprotocols, ","))
	}

	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, xerrors.Errorf("failed to send handshake request: %w", err)
	}
	defer func() {
		if err != nil {
			// We read a bit of the body for easier debugging.
			r := io.LimitReader(resp.Body, 1024)
			b, _ := ioutil.ReadAll(r)
			resp.Body.Close()
			resp.Body = ioutil.NopCloser(bytes.NewReader(b))
		}
	}()

	err = verifyServerResponse(resp)
	if err != nil {
		return nil, resp, err
	}

	rwc, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		return nil, resp, xerrors.Errorf("response body is not a read write closer: %T", rwc)
	}

	c := &Conn{
		subprotocol: resp.Header.Get("Sec-WebSocket-Protocol"),
		br:          bufio.NewReader(rwc),
		bw:          bufio.NewWriter(rwc),
		closer:      rwc,
		client:      true,
	}
	c.init()

	return c, resp, nil
}

func verifyServerResponse(resp *http.Response) error {
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return xerrors.Errorf("expected handshake response status code %v but got %v", http.StatusSwitchingProtocols, resp.StatusCode)
	}

	if !headerValuesContainsToken(resp.Header, "Connection", "Upgrade") {
		return xerrors.Errorf("websocket protocol violation: Connection header %q does not contain Upgrade", resp.Header.Get("Connection"))
	}

	if !headerValuesContainsToken(resp.Header, "Upgrade", "WebSocket") {
		return xerrors.Errorf("websocket protocol violation: Upgrade header %q does not contain websocket", resp.Header.Get("Upgrade"))
	}

	// We do not care about Sec-WebSocket-Accept because it does not matter.
	// See the secWebSocketKey global variable.

	return nil
}
