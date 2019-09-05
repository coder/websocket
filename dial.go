package websocket

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// DialOptions represents the options available to pass to Dial.
type DialOptions struct {
	// HTTPClient is the http client used for the handshake.
	// Its Transport must return writable bodies
	// for WebSocket handshakes.
	// http.Transport does this correctly beginning with Go 1.12.
	HTTPClient *http.Client

	// HTTPHeader specifies the HTTP headers included in the handshake request.
	HTTPHeader http.Header

	// Subprotocols lists the subprotocols to negotiate with the server.
	Subprotocols []string
}

// Dial performs a WebSocket handshake on the given url with the given options.
// The response is the WebSocket handshake response from the server.
// If an error occurs, the returned response may be non nil. However, you can only
// read the first 1024 bytes of its body.
//
// You never need to close the resp.Body yourself.
//
// This function requires at least Go 1.12 to succeed as it uses a new feature
// in net/http to perform WebSocket handshakes and get a writable body
// from the transport. See https://github.com/golang/go/issues/26937#issuecomment-415855861
func Dial(ctx context.Context, u string, opts *DialOptions) (*Conn, *http.Response, error) {
	c, r, err := dial(ctx, u, opts)
	if err != nil {
		return nil, r, fmt.Errorf("failed to websocket dial: %w", err)
	}
	return c, r, nil
}

func dial(ctx context.Context, u string, opts *DialOptions) (_ *Conn, _ *http.Response, err error) {
	if opts == nil {
		opts = &DialOptions{}
	}

	// Shallow copy to ensure defaults do not affect user passed options.
	opts2 := *opts
	opts = &opts2

	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.HTTPClient.Timeout > 0 {
		return nil, nil, fmt.Errorf("use context for cancellation instead of http.Client.Timeout; see https://github.com/nhooyr/websocket/issues/67")
	}
	if opts.HTTPHeader == nil {
		opts.HTTPHeader = http.Header{}
	}

	parsedURL, err := url.Parse(u)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse url: %w", err)
	}

	switch parsedURL.Scheme {
	case "ws":
		parsedURL.Scheme = "http"
	case "wss":
		parsedURL.Scheme = "https"
	default:
		return nil, nil, fmt.Errorf("unexpected url scheme: %q", parsedURL.Scheme)
	}

	req, _ := http.NewRequest("GET", parsedURL.String(), nil)
	req = req.WithContext(ctx)
	req.Header = opts.HTTPHeader
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", makeSecWebSocketKey())
	if len(opts.Subprotocols) > 0 {
		req.Header.Set("Sec-WebSocket-Protocol", strings.Join(opts.Subprotocols, ","))
	}

	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to send handshake request: %w", err)
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

	err = verifyServerResponse(req, resp)
	if err != nil {
		return nil, resp, err
	}

	rwc, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		return nil, resp, fmt.Errorf("response body is not a io.ReadWriteCloser: %T", rwc)
	}

	c := &Conn{
		subprotocol: resp.Header.Get("Sec-WebSocket-Protocol"),
		br:          getBufioReader(rwc),
		bw:          getBufioWriter(rwc),
		closer:      rwc,
		client:      true,
	}
	c.extractBufioWriterBuf(rwc)
	c.init()

	return c, resp, nil
}

func verifyServerResponse(r *http.Request, resp *http.Response) error {
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("expected handshake response status code %v but got %v", http.StatusSwitchingProtocols, resp.StatusCode)
	}

	if !headerValuesContainsToken(resp.Header, "Connection", "Upgrade") {
		return fmt.Errorf("websocket protocol violation: Connection header %q does not contain Upgrade", resp.Header.Get("Connection"))
	}

	if !headerValuesContainsToken(resp.Header, "Upgrade", "WebSocket") {
		return fmt.Errorf("websocket protocol violation: Upgrade header %q does not contain websocket", resp.Header.Get("Upgrade"))
	}

	if resp.Header.Get("Sec-WebSocket-Accept") != secWebSocketAccept(r.Header.Get("Sec-WebSocket-Key")) {
		return fmt.Errorf("websocket protocol violation: invalid Sec-WebSocket-Accept %q, key %q",
			resp.Header.Get("Sec-WebSocket-Accept"),
			r.Header.Get("Sec-WebSocket-Key"),
		)
	}

	return nil
}

// The below pools can only be used by the client because http.Hijacker will always
// have a bufio.Reader/Writer for us so it doesn't make sense to use a pool on top.

var bufioReaderPool = sync.Pool{
	New: func() interface{} {
		return bufio.NewReader(nil)
	},
}

func getBufioReader(r io.Reader) *bufio.Reader {
	br := bufioReaderPool.Get().(*bufio.Reader)
	br.Reset(r)
	return br
}

func returnBufioReader(br *bufio.Reader) {
	bufioReaderPool.Put(br)
}

var bufioWriterPool = sync.Pool{
	New: func() interface{} {
		return bufio.NewWriter(nil)
	},
}

func getBufioWriter(w io.Writer) *bufio.Writer {
	bw := bufioWriterPool.Get().(*bufio.Writer)
	bw.Reset(w)
	return bw
}

func returnBufioWriter(bw *bufio.Writer) {
	bufioWriterPool.Put(bw)
}

func makeSecWebSocketKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}
