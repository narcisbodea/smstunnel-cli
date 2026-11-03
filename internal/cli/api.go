package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"runtime"
	"strings"
	"time"

	smstunnel "github.com/narcisbodea/smstunnel-go"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

// The CLI calls SMSTunnel through smstunnel-go's Client. The Node version prints SMSTunnel's answers as they were
// sent (--json) and decides from their raw JSON (a missing member is not a null one), so the transport given to the
// library with WithHTTPClient keeps every answer as it came, and makes each request the one the Node version
// sends: the same URL with the query in the same order, the same JSON body, the same headers.

const (
	requestTimeout = 30 * time.Second // no answer within it: a timeout (socket inactivity in the Node version)
	maxAnswerBytes = 5 * 1024 * 1024  // larger answers are dropped (a network error)
)

// exchange is what happened on the wire for one call.
type exchange struct {
	started bool
	wrote   bool // the whole request was written to the connection (Node's "finish")
	status  int
	header  http.Header
	body    []byte
	err     error // no answer, or the answer broke off
}

// intent is the request the Node version sends for a call.
type intent struct {
	method  string
	path    string // added to the API address
	query   [][2]string
	body    []byte
	headers [][2]string // extra headers, first
	ex      *exchange
}

type intentKey struct{}

type apiClient struct {
	apiBase   string
	base      *js.URL
	key       string
	userAgent string
	lib       *smstunnel.Client
	now       func() time.Time
}

func (c *runContext) client(key string) (*apiClient, error) {
	base, err := js.ParseURL(c.apiBase)
	if err != nil {
		return nil, err
	}
	ac := &apiClient{
		apiBase:   c.apiBase,
		base:      base,
		key:       key,
		userAgent: "smstunnel-cli/" + Version + " (" + runtime.Version() + "; " + c.env.GOOS + ") smstunnel-go/" + smstunnel.Version,
		now:       c.env.Now,
	}
	transport := c.env.Transport
	if transport == nil {
		transport = newNodeLikeTransport()
	}
	hc := &http.Client{
		Transport:     &wireTransport{base: transport, client: ac},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	// The key goes in the X-API-Key header exactly as the Node version sends it (the transport sets it). The library
	// refuses keys with characters a key never has; SMSTUNNEL_API_KEY is not checked by the Node version, so such a
	// key still goes to SMSTunnel, which refuses it with 401.
	libKey := key
	if !libraryTakesKey(key) {
		libKey = "smstunnel-cli-key-placeholder"
	}
	lib, err := smstunnel.NewClient(libKey,
		smstunnel.WithBaseURL(base.Scheme+"://"+base.HostPort()),
		smstunnel.WithHTTPClient(hc),
		smstunnel.WithTimeout(0), // the transport has the Node version's 30-second inactivity timeout
	)
	if err != nil {
		return nil, err
	}
	ac.lib = lib
	return ac, nil
}

func libraryTakesKey(key string) bool {
	k := strings.TrimSpace(key)
	if k == "" || k != key {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < '!' || k[i] > '~' {
			return false
		}
	}
	return true
}

// answer is a successful answer: the JSON body (an object or an array) and whether it was replayed.
type answer struct {
	body     js.Value
	replayed bool
}

// do runs one call: the library method in call, with the request in in.
func (c *apiClient) do(in *intent, call func(ctx context.Context) error) (*answer, error) {
	in.ex = &exchange{}
	ctx := context.WithValue(context.Background(), intentKey{}, in)
	libErr := call(ctx)
	if !in.ex.started {
		// The library refused the call before sending it.
		if libErr == nil {
			libErr = errors.New("no request was made")
		}
		return nil, fmt.Errorf("smstunnel-go did not send the request: %w", libErr)
	}
	return c.interpret(in.ex)
}

func (c *apiClient) getDevices() (*answer, error) {
	return c.do(&intent{method: http.MethodGet, path: "/devices"}, func(ctx context.Context) error {
		_, err := c.lib.ListPhones(ctx)
		return err
	})
}

func (c *apiClient) getStatus(id string) (*answer, error) {
	return c.do(&intent{method: http.MethodGet, path: "/sms/status/" + id}, func(ctx context.Context) error {
		_, err := c.lib.GetMessageStatus(ctx, id)
		return err
	})
}

func (c *apiClient) getUsage() (*answer, error) {
	return c.do(&intent{method: http.MethodGet, path: "/account/usage"}, func(ctx context.Context) error {
		_, err := c.lib.GetUsage(ctx)
		return err
	})
}

type receivedQuery struct {
	limit    int
	since    float64 // NaN: none
	sinceSet bool
	phone    string
	deviceID js.Value
}

func (c *apiClient) getReceived(q receivedQuery) (*answer, error) {
	in := &intent{method: http.MethodGet, path: "/sms/received"}
	in.query = append(in.query, [2]string{"limit", js.NumberToString(float64(q.limit))})
	lib := smstunnel.ReceivedTextsQuery{Limit: q.limit, Phone: q.phone}
	if q.sinceSet {
		if !js.IsValid(q.since) {
			// toISOString of an invalid date throws in the Node version.
			return nil, &jsError{name: "RangeError", message: "Invalid time value"}
		}
		in.query = append(in.query, [2]string{"since", js.ISOString(q.since)})
		lib.Since = time.UnixMilli(int64(q.since)).UTC()
	}
	if q.phone != "" {
		in.query = append(in.query, [2]string{"phone", q.phone})
	}
	// URLSearchParams gets String(value) unless the value is undefined or the empty string.
	if !q.deviceID.IsUndefined() && !q.deviceID.StrictEquals("") {
		id := q.deviceID.String()
		in.query = append(in.query, [2]string{"deviceId", id})
		lib.DeviceID = id
	}
	return c.do(in, func(ctx context.Context) error {
		_, err := c.lib.ListReceivedTexts(ctx, lib)
		return err
	})
}

// sendText posts one text: body is {to, message[, deviceId][, defaultCountryCode]}.
func (c *apiClient) sendText(code bool, body js.Value, idempotencyKey string) (*answer, error) {
	in := &intent{method: http.MethodPost, path: "/sms/send", body: []byte(js.Stringify(body, ""))}
	if code {
		in.path = "/sms/send-2fa"
	}
	if idempotencyKey != "" {
		in.headers = append(in.headers, [2]string{"Idempotency-Key", idempotencyKey})
	}
	to, message := body.Get("to").String(), body.Get("message").String()
	if strings.TrimSpace(message) == "" {
		// The library refuses a text that is blank by Go's rules (only U+0085 and spaces, say), which the Node
		// version sends; the transport sends the text itself in any case.
		message = "-"
	}
	var opts []smstunnel.SendOption
	if id := body.Get("deviceId"); !id.IsUndefined() {
		opts = append(opts, smstunnel.WithDeviceID(id.String()))
	}
	if cc := body.Get("defaultCountryCode"); !cc.IsUndefined() {
		opts = append(opts, smstunnel.WithCountryCode(cc.String()))
	}
	if idempotencyKey != "" {
		opts = append(opts, smstunnel.WithIdempotencyKey(idempotencyKey))
	}
	return c.do(in, func(ctx context.Context) error {
		var err error
		if code {
			_, err = c.lib.SendSignInCode(ctx, to, message, opts...)
		} else {
			_, err = c.lib.SendSMS(ctx, to, message, opts...)
		}
		return err
	})
}

// interpret reads an exchange the way the Node version's ApiClient.request does.
func (c *apiClient) interpret(ex *exchange) (*answer, error) {
	if ex.err != nil {
		code, timedOut := networkCode(ex.err)
		kind := "network"
		if timedOut {
			kind = "timeout"
		}
		return nil, &apiError{kind: kind, message: ex.err.Error(), networkCode: code,
			mayHaveArrived: ex.wrote && !neverArrived[code]}
	}
	text := js.DecodeUTF8(ex.body)
	replayed := js.ToLower(js.Trim(latin1(strings.Join(ex.header.Values("Idempotent-Replayed"), ", ")))) == "true"
	parsed := js.UndefinedValue
	if js.Trim(text) != "" {
		if v, err := js.Parse(text); err == nil {
			parsed = v
		}
	}
	status := js.IntValue(ex.status)
	switch {
	case ex.status >= 300 && ex.status < 400:
		return nil, &apiError{kind: "redirect", status: status, message: "Redirect", mayHaveArrived: true}
	case ex.status >= 400 || ex.status < 200:
		e := &apiError{kind: "http", status: status, message: errorMessageFrom(parsed, text, ex.status),
			mayHaveArrived: true, replayed: replayed}
		if parsed.Kind() == js.Object {
			if v := parsed.Get("code"); v.IsString() {
				e.code = v
			}
			if v := parsed.Get("permission"); v.IsString() {
				e.permission = v
			}
		}
		if values := ex.header.Values("Retry-After"); len(values) > 0 {
			if n, ok := parseRetryAfter(latin1(values[0]), c.now()); ok {
				e.retryAfter = js.NumberValue(n)
			}
		}
		return nil, e
	}
	if parsed.Kind() != js.Object && parsed.Kind() != js.Array {
		return nil, &apiError{kind: "invalid-response", status: status, message: "The answer was not JSON", mayHaveArrived: true}
	}
	if success := parsed.Get("success"); success.Kind() == js.Bool && !success.BoolVal() {
		return nil, &apiError{kind: "http", status: status, message: errorMessageFrom(parsed, text, ex.status),
			mayHaveArrived: true, replayed: replayed}
	}
	return &answer{body: parsed, replayed: replayed}, nil
}

// latin1 reads header bytes the way Node.js does: one character per byte.
func latin1(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		b.WriteRune(rune(s[i]))
	}
	return b.String()
}

// errorMessageFrom is SMSTunnel's words for an error: a "message" list is joined, else "message", else "error",
// else a short plain-text body, else "HTTP <status>".
func errorMessageFrom(body js.Value, text string, status int) string {
	if body.Kind() == js.Object {
		if m := body.Get("message"); m.Kind() == js.Array {
			var parts []string
			for _, item := range m.Items() {
				if item.IsString() && js.Trim(item.Str()) != "" {
					parts = append(parts, item.Str())
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "; ")
			}
		}
		if m := body.Get("message"); m.IsString() && js.Trim(m.Str()) != "" {
			return js.Trim(m.Str())
		}
		if e := body.Get("error"); e.IsString() && js.Trim(e.Str()) != "" {
			return js.Trim(e.Str())
		}
	}
	plain := js.Trim(text)
	if plain != "" && js.Length(plain) <= 300 && !strings.HasPrefix(plain, "<") {
		return plain
	}
	return "HTTP " + js.NumberToString(float64(status))
}

// parseRetryAfter: digits are seconds; an HTTP date is the seconds from now (at least 1). ok is false otherwise.
func parseRetryAfter(value string, now time.Time) (float64, bool) {
	trimmed := js.Trim(value)
	if trimmed == "" {
		return 0, false
	}
	digits := true
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] < '0' || trimmed[i] > '9' {
			digits = false
			break
		}
	}
	if digits {
		return js.ToNumber(trimmed), true
	}
	date := js.ParseDate(trimmed)
	if !js.IsValid(date) {
		return 0, false
	}
	return math.Max(1, math.Ceil((date-float64(now.UnixMilli()))/1000)), true
}

// ---- the transport ----

type wireTransport struct {
	base   http.RoundTripper
	client *apiClient
}

func (t *wireTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	in, _ := req.Context().Value(intentKey{}).(*intent)
	if in == nil {
		return nil, errors.New("smstunnel-cli: a request without its intent")
	}
	ex := in.ex
	ex.started = true
	base := t.client.base
	target := &url.URL{Scheme: base.Scheme, Host: base.HostPort()}
	path := base.Pathname + in.path
	if strings.HasPrefix(path, "//") {
		target.Path = path // a request target must not start with "//": let net/url write it
	} else {
		target.Opaque = path
	}
	var query []string
	for _, kv := range in.query {
		query = append(query, js.FormEncode(kv[0])+"="+js.FormEncode(kv[1]))
	}
	target.RawQuery = strings.Join(query, "&")

	trace := &httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				ex.wrote = true
			}
		},
	}
	out := (&http.Request{
		Method:     in.method,
		URL:        target,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}).WithContext(httptrace.WithClientTrace(req.Context(), trace))
	header := http.Header{}
	for _, kv := range in.headers {
		header[kv[0]] = []string{kv[1]}
	}
	header["X-API-Key"] = []string{toLatin1Bytes(t.client.key)}
	header["Accept"] = []string{"application/json"}
	header["User-Agent"] = []string{t.client.userAgent}
	if in.body != nil {
		header["Content-Type"] = []string{"application/json"}
		body := in.body
		out.Body = io.NopCloser(bytes.NewReader(body))
		out.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
		out.ContentLength = int64(len(body))
	}
	out.Header = header
	if bad := invalidHeaderValue(t.client.key); bad {
		// Node.js refuses such a header before connecting (ERR_INVALID_CHAR).
		ex.err = &codedError{code: "ERR_INVALID_CHAR", msg: `Invalid character in header content ["X-API-Key"]`}
		return nil, ex.err
	}
	resp, err := t.base.RoundTrip(out)
	if err != nil {
		ex.err = err
		return nil, err
	}
	ex.status = resp.StatusCode
	ex.header = resp.Header
	resp.Body = &capturedBody{body: resp.Body, ex: ex}
	return resp, nil
}

// invalidHeaderValue is Node.js's check of a header value: only tab, printable ASCII and U+0080..U+00FF.
func invalidHeaderValue(v string) bool {
	for _, r := range js.CodePoints(v) {
		if r == '\t' || (r >= 0x20 && r <= 0x7E) || (r >= 0x80 && r <= 0xFF) {
			continue
		}
		return true
	}
	return false
}

// toLatin1Bytes writes a header value the way Node.js does (one byte per character, latin1).
func toLatin1Bytes(v string) string {
	var b []byte
	for _, r := range js.CodePoints(v) {
		b = append(b, byte(r))
	}
	return string(b)
}

// capturedBody keeps what the library reads of an answer, and fails past maxAnswerBytes.
type capturedBody struct {
	body io.ReadCloser
	ex   *exchange
}

func (c *capturedBody) Read(p []byte) (int, error) {
	n, err := c.body.Read(p)
	c.ex.body = append(c.ex.body, p[:n]...)
	if len(c.ex.body) > maxAnswerBytes {
		c.ex.err = &codedError{code: "ETOOLARGE", msg: "The answer was too large"}
		return n, c.ex.err
	}
	if err != nil && err != io.EOF && c.ex.err == nil {
		c.ex.err = err
	}
	return n, err
}

func (c *capturedBody) Close() error { return c.body.Close() }

type codedError struct{ code, msg string }

func (e *codedError) Error() string { return e.msg }

// newNodeLikeTransport connects the way Node.js's http module does for the Node version: HTTP/1.1, no proxy,
// no compression, a fresh connection per request (so nothing is ever sent again on a reused one), and a 30-second
// limit on silence (connecting, then each read and write).
func newNodeLikeTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: requestTimeout}
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := dialer.DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			return &idleTimeoutConn{Conn: conn}, nil
		},
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    requestTimeout,
		DisableKeepAlives:      true,
		DisableCompression:     true,
		ForceAttemptHTTP2:      false,
		TLSNextProto:           map[string]func(string, *tls.Conn) http.RoundTripper{},
		MaxResponseHeaderBytes: 16 * 1024,
	}
}

type idleTimeoutConn struct{ net.Conn }

func (c *idleTimeoutConn) Read(p []byte) (int, error) {
	_ = c.Conn.SetReadDeadline(time.Now().Add(requestTimeout))
	return c.Conn.Read(p)
}

func (c *idleTimeoutConn) Write(p []byte) (int, error) {
	_ = c.Conn.SetWriteDeadline(time.Now().Add(requestTimeout))
	return c.Conn.Write(p)
}

// neverArrived are the failures that happen before anything reaches SMSTunnel.
var neverArrived = map[string]bool{
	"ECONNREFUSED": true, "ENOTFOUND": true, "EAI_AGAIN": true, "EHOSTUNREACH": true, "ENETUNREACH": true,
	"EADDRNOTAVAIL": true, "CERT_HAS_EXPIRED": true, "DEPTH_ZERO_SELF_SIGNED_CERT": true,
	"SELF_SIGNED_CERT_IN_CHAIN": true, "UNABLE_TO_VERIFY_LEAF_SIGNATURE": true,
	"UNABLE_TO_GET_ISSUER_CERT_LOCALLY": true, "ERR_TLS_CERT_ALTNAME_INVALID": true,
}
