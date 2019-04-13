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

// DialOption represents a dial option that can be passed to Dial.
// The implementations are printable for easy debugging.
type DialOption interface {
	dialOption()
}

type dialHTTPClient http.Client

func (o dialHTTPClient) dialOption() {}

// DialHTTPClient is the http client used for the handshake.
// Its Transport must use HTTP/1.1 and must return writable bodies
// for WebSocket handshakes.
// http.Transport does this correctly.
func DialHTTPClient(hc *http.Client) DialOption {
	return (*dialHTTPClient)(hc)
}

type dialHeader http.Header

func (o dialHeader) dialOption() {}

// DialHeader are the HTTP headers included in the handshake request.
func DialHeader(h http.Header) DialOption {
	return dialHeader(h)
}

type dialSubprotocols []string

func (o dialSubprotocols) dialOption() {}

// DialSubprotocols accepts a slice of protcols to include in the Sec-WebSocket-Protocol header.
func DialSubprotocols(subprotocols ...string) DialOption {
	return dialSubprotocols(subprotocols)
}

// We use this key for all client requests as the Sec-WebSocket-Key header is useless.
// See https://stackoverflow.com/a/37074398/4283659.
// We also use the same mask key for every message as it too does not make a difference.
var secWebSocketKey = base64.StdEncoding.EncodeToString(make([]byte, 16))

// Dial performs a WebSocket handshake on the given url with the given options.
func Dial(ctx context.Context, u string, opts ...DialOption) (_ *Conn, _ *http.Response, err error) {
	httpClient := http.DefaultClient
	var subprotocols []string
	header := http.Header{}
	for _, o := range opts {
		switch o := o.(type) {
		case dialSubprotocols:
			subprotocols = o
		case dialHeader:
			header = http.Header(o)
		case *dialHTTPClient:
			httpClient = (*http.Client)(o)
		}
	}

	parsedURL, err := url.Parse(u)
	if err != nil {
		return nil, nil, xerrors.Errorf("failed to parse websocket url: %w", err)
	}

	switch parsedURL.Scheme {
	case "ws":
		parsedURL.Scheme = "http"
	case "wss":
		parsedURL.Scheme = "https"
	default:
		return nil, nil, xerrors.Errorf("unknown scheme in url: %q", parsedURL.Scheme)
	}

	req, _ := http.NewRequest("GET", parsedURL.String(), nil)
	req = req.WithContext(ctx)
	req.Header = header
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", secWebSocketKey)
	if len(subprotocols) > 0 {
		req.Header.Set("Sec-WebSocket-Protocol", strings.Join(subprotocols, ","))
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		respBody := resp.Body
		if err != nil {
			// We read a bit of the body for better debugging.
			r := io.LimitReader(resp.Body, 1024)
			b, _ := ioutil.ReadAll(r)
			resp.Body = ioutil.NopCloser(bytes.NewReader(b))
			respBody.Close()
		}
	}()

	err = verifyServerResponse(resp)
	if err != nil {
		return nil, resp, err
	}

	rwc, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		return nil, resp, xerrors.Errorf("websocket: body is not a read write closer but should be: %T", rwc)
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
		return xerrors.Errorf("websocket: expected status code %v but got %v", http.StatusSwitchingProtocols, resp.StatusCode)
	}

	if !headerValuesContainsToken(resp.Header, "Connection", "Upgrade") {
		return xerrors.Errorf("websocket: protocol violation: Connection header does not contain Upgrade: %q", resp.Header.Get("Connection"))
	}

	if !headerValuesContainsToken(resp.Header, "Upgrade", "WebSocket") {
		return xerrors.Errorf("websocket: protocol violation: Upgrade header does not contain websocket: %q", resp.Header.Get("Upgrade"))
	}

	// We do not care about Sec-WebSocket-Accept because it does not matter.
	// See the secWebSocketKey global variable.

	return nil
}
