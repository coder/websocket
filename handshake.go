// +build !js

package websocket

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
)

// AcceptOptions represents the options available to pass to Accept.
type AcceptOptions struct {
	// Subprotocols lists the websocket subprotocols that Accept will negotiate with a client.
	// The empty subprotocol will always be negotiated as per RFC 6455. If you would like to
	// reject it, close the connection if c.Subprotocol() == "".
	Subprotocols []string

	// InsecureSkipVerify disables Accept's origin verification
	// behaviour. By default Accept only allows the handshake to
	// succeed if the javascript that is initiating the handshake
	// is on the same domain as the server. This is to prevent CSRF
	// attacks when secure data is stored in a cookie as there is no same
	// origin policy for WebSockets. In other words, javascript from
	// any domain can perform a WebSocket dial on an arbitrary server.
	// This dial will include cookies which means the arbitrary javascript
	// can perform actions as the authenticated user.
	//
	// See https://stackoverflow.com/a/37837709/4283659
	//
	// The only time you need this is if your javascript is running on a different domain
	// than your WebSocket server.
	// Think carefully about whether you really need this option before you use it.
	// If you do, remember that if you store secure data in cookies, you wil need to verify the
	// Origin header yourself otherwise you are exposing yourself to a CSRF attack.
	InsecureSkipVerify bool

	// Compression sets the compression options.
	// By default, compression is disabled.
	// See docs on the CompressionOptions type.
	Compression *CompressionOptions
}

func verifyClientRequest(w http.ResponseWriter, r *http.Request) error {
	if !r.ProtoAtLeast(1, 1) {
		err := fmt.Errorf("websocket protocol violation: handshake request must be at least HTTP/1.1: %q", r.Proto)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	if !headerContainsToken(r.Header, "Connection", "Upgrade") {
		err := fmt.Errorf("websocket protocol violation: Connection header %q does not contain Upgrade", r.Header.Get("Connection"))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	if !headerContainsToken(r.Header, "Upgrade", "WebSocket") {
		err := fmt.Errorf("websocket protocol violation: Upgrade header %q does not contain websocket", r.Header.Get("Upgrade"))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	if r.Method != "GET" {
		err := fmt.Errorf("websocket protocol violation: handshake request method is not GET but %q", r.Method)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		err := fmt.Errorf("unsupported websocket protocol version (only 13 is supported): %q", r.Header.Get("Sec-WebSocket-Version"))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	if r.Header.Get("Sec-WebSocket-Key") == "" {
		err := errors.New("websocket protocol violation: missing Sec-WebSocket-Key")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return err
	}

	return nil
}

// Accept accepts a WebSocket handshake from a client and upgrades the
// the connection to a WebSocket.
//
// Accept will reject the handshake if the Origin domain is not the same as the Host unless
// the InsecureSkipVerify option is set. In other words, by default it does not allow
// cross origin requests.
//
// If an error occurs, Accept will always write an appropriate response so you do not
// have to.
func Accept(w http.ResponseWriter, r *http.Request, opts *AcceptOptions) (*Conn, error) {
	c, err := accept(w, r, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to accept websocket connection: %w", err)
	}
	return c, nil
}

func accept(w http.ResponseWriter, r *http.Request, opts *AcceptOptions) (*Conn, error) {
	if opts == nil {
		opts = &AcceptOptions{}
	}

	err := verifyClientRequest(w, r)
	if err != nil {
		return nil, err
	}

	if !opts.InsecureSkipVerify {
		err = authenticateOrigin(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return nil, err
		}
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		err = errors.New("passed ResponseWriter does not implement http.Hijacker")
		http.Error(w, http.StatusText(http.StatusNotImplemented), http.StatusNotImplemented)
		return nil, err
	}

	w.Header().Set("Upgrade", "websocket")
	w.Header().Set("Connection", "Upgrade")

	handleSecWebSocketKey(w, r)

	subproto := selectSubprotocol(r, opts.Subprotocols)
	if subproto != "" {
		w.Header().Set("Sec-WebSocket-Protocol", subproto)
	}

	var copts *CompressionOptions
	if opts.Compression != nil {
		copts, err = negotiateCompression(r.Header, opts.Compression)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return nil, err
		}
		if copts != nil {
			copts.setHeader(w.Header())
		}
	}

	w.WriteHeader(http.StatusSwitchingProtocols)

	netConn, brw, err := hj.Hijack()
	if err != nil {
		err = fmt.Errorf("failed to hijack connection: %w", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return nil, err
	}

	// https://github.com/golang/go/issues/32314
	b, _ := brw.Reader.Peek(brw.Reader.Buffered())
	brw.Reader.Reset(io.MultiReader(bytes.NewReader(b), netConn))

	c := &Conn{
		subprotocol: w.Header().Get("Sec-WebSocket-Protocol"),
		br:          brw.Reader,
		bw:          brw.Writer,
		closer:      netConn,
		copts:       copts,
	}
	c.init()

	return c, nil
}

func headerContainsToken(h http.Header, key, token string) bool {
	key = textproto.CanonicalMIMEHeaderKey(key)

	token = strings.ToLower(token)
	match := func(t string) bool {
		return t == token
	}

	for _, v := range h[key] {
		if searchHeaderTokens(v, match) != "" {
			return true
		}
	}

	return false
}

func headerTokenHasPrefix(h http.Header, key, prefix string) string {
	key = textproto.CanonicalMIMEHeaderKey(key)

	prefix = strings.ToLower(prefix)
	match := func(t string) bool {
		return strings.HasPrefix(t, prefix)
	}

	for _, v := range h[key] {
		found := searchHeaderTokens(v, match)
		if found != "" {
			return found
		}
	}

	return ""
}

func searchHeaderTokens(v string, match func(val string) bool) string {
	v = strings.TrimSpace(v)

	for _, v2 := range strings.Split(v, ",") {
		v2 = strings.TrimSpace(v2)
		v2 = strings.ToLower(v2)
		if match(v2) {
			return v2
		}
	}

	return ""
}

func selectSubprotocol(r *http.Request, subprotocols []string) string {
	for _, sp := range subprotocols {
		if headerContainsToken(r.Header, "Sec-WebSocket-Protocol", sp) {
			return sp
		}
	}
	return ""
}

var keyGUID = []byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11")

func handleSecWebSocketKey(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	w.Header().Set("Sec-WebSocket-Accept", secWebSocketAccept(key))
}

func secWebSocketAccept(secWebSocketKey string) string {
	h := sha1.New()
	h.Write([]byte(secWebSocketKey))
	h.Write(keyGUID)

	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func authenticateOrigin(r *http.Request) error {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("failed to parse Origin header %q: %w", origin, err)
	}
	if !strings.EqualFold(u.Host, r.Host) {
		return fmt.Errorf("request Origin %q is not authorized for Host %q", origin, r.Host)
	}
	return nil
}

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

	// Compression sets the compression options.
	// By default, compression is disabled.
	// See docs on the CompressionOptions type.
	Compression *CompressionOptions
}

// CompressionOptions describes the available compression options.
//
// See https://tools.ietf.org/html/rfc7692
//
// The NoContextTakeover variables control whether a flate.Writer or flate.Reader is allocated
// for every connection (context takeover) versus shared from a pool (no context takeover).
//
// The advantage to context takeover is more efficient compression as the sliding window from previous
// messages will be used instead of being reset between every message.
//
// The advantage to no context takeover is that the flate structures are allocated as needed
// and shared between connections instead of giving each connection a fixed flate.Writer and
// flate.Reader.
//
// See https://www.igvita.com/2013/11/27/configuring-and-optimizing-websocket-compression.
//
// Enabling compression will increase memory and CPU usage and should
// be profiled before enabling in production.
// See https://github.com/gorilla/websocket/issues/203
//
// This API is experimental and subject to change.
type CompressionOptions struct {
	// ClientNoContextTakeover controls whether the client should use context takeover.
	// See docs on CompressionOptions for discussion regarding context takeover.
	//
	// If set by the server, will guarantee that the client does not use context takeover.
	ClientNoContextTakeover bool

	// ServerNoContextTakeover controls whether the server should use context takeover.
	// See docs on CompressionOptions for discussion regarding context takeover.
	//
	// If set by the client, will guarantee that the server does not use context takeover.
	ServerNoContextTakeover bool

	// Level controls the compression level used.
	// Defaults to flate.BestSpeed.
	Level int

	// Threshold controls the minimum message size in bytes before compression is used.
	// Must not be greater than 4096 as that is the write buffer's size.
	//
	// Defaults to 256.
	Threshold int
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

func (opts *DialOptions) ensure() (*DialOptions, error) {
	if opts == nil {
		opts = &DialOptions{}
	} else {
		opts = &*opts
	}

	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.HTTPClient.Timeout > 0 {
		return nil, fmt.Errorf("use context for cancellation instead of http.Client.Timeout; see https://github.com/nhooyr/websocket/issues/67")
	}
	if opts.HTTPHeader == nil {
		opts.HTTPHeader = http.Header{}
	}

	return opts, nil
}

func dial(ctx context.Context, u string, opts *DialOptions) (_ *Conn, _ *http.Response, err error) {
	opts, err = opts.ensure()
	if err != nil {
		return nil, nil, err
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
	secWebSocketKey, err := makeSecWebSocketKey()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate Sec-WebSocket-Key: %w", err)
	}
	req.Header.Set("Sec-WebSocket-Key", secWebSocketKey)
	if len(opts.Subprotocols) > 0 {
		req.Header.Set("Sec-WebSocket-Protocol", strings.Join(opts.Subprotocols, ","))
	}
	if opts.Compression != nil {
		opts.Compression.setHeader(req.Header)
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

	copts, err := verifyServerResponse(req, resp, opts)
	if err != nil {
		return nil, resp, err
	}

	rwc, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		return nil, resp, fmt.Errorf("response body is not a io.ReadWriteCloser: %T", resp.Body)
	}

	c := &Conn{
		subprotocol: resp.Header.Get("Sec-WebSocket-Protocol"),
		br:          getBufioReader(rwc),
		bw:          getBufioWriter(rwc),
		closer:      rwc,
		client:      true,
		copts:       copts,
	}
	c.extractBufioWriterBuf(rwc)
	c.init()

	return c, resp, nil
}

func verifyServerResponse(r *http.Request, resp *http.Response, opts *DialOptions) (*CompressionOptions, error) {
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

	var copts *CompressionOptions
	if opts.Compression != nil {
		var err error
		copts, err = negotiateCompression(resp.Header, opts.Compression)
		if err != nil {
			return nil, err
		}
	}

	return copts, nil
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

func makeSecWebSocketKey() (string, error) {
	b := make([]byte, 16)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		return "", fmt.Errorf("failed to read random data from rand.Reader: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func negotiateCompression(h http.Header, copts *CompressionOptions) (*CompressionOptions, error) {
	deflate := headerTokenHasPrefix(h, "Sec-WebSocket-Extensions", "permessage-deflate")
	if deflate == "" {
		return nil, nil
	}

	// Ensures our changes do not modify the real compression options.
	copts = &*copts

	params := strings.Split(deflate, ";")
	for i := range params {
		params[i] = strings.TrimSpace(params[i])
	}

	if params[0] != "permessage-deflate" {
		return nil, fmt.Errorf("unexpected header format for permessage-deflate extension: %q", deflate)
	}

	for _, p := range params[1:] {
		switch p {
		case "client_no_context_takeover":
			copts.ClientNoContextTakeover = true
			continue
		case "server_no_context_takeover":
			copts.ServerNoContextTakeover = true
			continue
		case "client_max_window_bits", "server-max-window-bits":
			server := h.Get("Sec-WebSocket-Key") != ""
			if server {
				// If we are the server, we are allowed to ignore these parameters.
				// However, if we are the client, we must obey them but because of
				// https://github.com/golang/go/issues/3155 we cannot.
				continue
			}
		}
		return nil, fmt.Errorf("unsupported permessage-deflate parameter %q in header: %q", p, deflate)
	}

	return copts, nil
}

func (copts *CompressionOptions) setHeader(h http.Header) {
	s := "permessage-deflate"
	if copts.ClientNoContextTakeover {
		s += "; client_no_context_takeover"
	}
	if copts.ServerNoContextTakeover {
		s += "; server_no_context_takeover"
	}
	h.Set("Sec-WebSocket-Extensions", s)
}
