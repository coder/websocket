package websocket

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
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

	// See docs on CompressionMode.
	CompressionMode CompressionMode
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

func (opts *DialOptions) ensure() *DialOptions {
	if opts == nil {
		opts = &DialOptions{}
	} else {
		opts = &*opts
	}

	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.HTTPHeader == nil {
		opts.HTTPHeader = http.Header{}
	}

	return opts
}

func dial(ctx context.Context, u string, opts *DialOptions) (_ *Conn, _ *http.Response, err error) {
	opts = opts.ensure()

	if opts.HTTPClient.Timeout > 0 {
		return nil, nil, errors.New("use context for cancellation instead of http.Client.Timeout; see https://github.com/nhooyr/websocket/issues/67")
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
	secWebSocketKey, err := secWebSocketKey()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate Sec-WebSocket-Key: %w", err)
	}
	req.Header.Set("Sec-WebSocket-Key", secWebSocketKey)
	if len(opts.Subprotocols) > 0 {
		req.Header.Set("Sec-WebSocket-Protocol", strings.Join(opts.Subprotocols, ","))
	}
	if opts.CompressionMode != CompressionDisabled {
		copts := opts.CompressionMode.opts()
		copts.setHeader(req.Header)
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

	copts, err := verifyServerResponse(req, resp)
	if err != nil {
		return nil, resp, err
	}

	rwc, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		return nil, resp, fmt.Errorf("response body is not a io.ReadWriteCloser: %T", rwc)
	}

	return newConn(connConfig{
		subprotocol: resp.Header.Get("Sec-WebSocket-Protocol"),
		rwc:         rwc,
		client:      true,
		copts:       copts,
		br:          getBufioReader(rwc),
		bw:          getBufioWriter(rwc),
	}), resp, nil
}

func secWebSocketKey() (string, error) {
	b := make([]byte, 16)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		return "", fmt.Errorf("failed to read random data from rand.Reader: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func verifyServerResponse(r *http.Request, resp *http.Response) (*compressionOptions, error) {
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, fmt.Errorf("expected handshake response status code %v but got %v", http.StatusSwitchingProtocols, resp.StatusCode)
	}

	if !headerContainsToken(resp.Header, "Connection", "Upgrade") {
		return nil, fmt.Errorf("websocket protocol violation: Connection header %q does not contain Upgrade", resp.Header.Get("Connection"))
	}

	if !headerContainsToken(resp.Header, "Upgrade", "WebSocket") {
		return nil, fmt.Errorf("websocket protocol violation: Upgrade header %q does not contain websocket", resp.Header.Get("Upgrade"))
	}

	if resp.Header.Get("Sec-WebSocket-Accept") != secWebSocketAccept(r.Header.Get("Sec-WebSocket-Key")) {
		return nil, fmt.Errorf("websocket protocol violation: invalid Sec-WebSocket-Accept %q, key %q",
			resp.Header.Get("Sec-WebSocket-Accept"),
			r.Header.Get("Sec-WebSocket-Key"),
		)
	}

	if proto := resp.Header.Get("Sec-WebSocket-Protocol"); proto != "" && !headerContainsToken(r.Header, "Sec-WebSocket-Protocol", proto) {
		return nil, fmt.Errorf("websocket protocol violation: unexpected Sec-WebSocket-Protocol from server: %q", proto)
	}

	copts, err := verifyServerExtensions(resp.Header)
	if err != nil {
		return nil, err
	}

	return copts, nil
}

func verifyServerExtensions(h http.Header) (*compressionOptions, error) {
	exts := websocketExtensions(h)
	if len(exts) == 0 {
		return nil, nil
	}

	ext := exts[0]
	if ext.name != "permessage-deflate" {
		return nil, fmt.Errorf("unexpected extension from server: %q", ext)
	}

	if len(exts) > 1 {
		return nil, fmt.Errorf("unexpected extra extensions from server: %+v", exts[1:])
	}

	copts := &compressionOptions{}
	for _, p := range ext.params {
		switch p {
		case "client_no_context_takeover":
			copts.clientNoContextTakeover = true
			continue
		case "server_no_context_takeover":
			copts.serverNoContextTakeover = true
			continue
		}

		return nil, fmt.Errorf("unsupported permessage-deflate parameter: %q", p)
	}

	return copts, nil
}

var readerPool sync.Pool

func getBufioReader(r io.Reader) *bufio.Reader {
	br, ok := readerPool.Get().(*bufio.Reader)
	if !ok {
		return bufio.NewReader(r)
	}
	br.Reset(r)
	return br
}

func putBufioReader(br *bufio.Reader) {
	readerPool.Put(br)
}

var writerPool sync.Pool

func getBufioWriter(w io.Writer) *bufio.Writer {
	bw, ok := writerPool.Get().(*bufio.Writer)
	if !ok {
		return bufio.NewWriter(w)
	}
	bw.Reset(w)
	return bw
}

func putBufioWriter(bw *bufio.Writer) {
	writerPool.Put(bw)
}
