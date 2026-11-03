package cli

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

// mockAPI stands in for the SMSTunnel REST API, as the Node version's test/e2e/mock-server.mjs does: the same
// routes, bodies and errors, and a few numbers that trigger each refusal.

var mockPhones = []struct {
	id, name string
	online   bool
}{
	{"a1a1a1a1a1a1a1a1a1a1a1a1", "Desk phone", true},
	{"b2b2b2b2b2b2b2b2b2b2b2b2", "Spare phone", false},
	{"c3c3c3c3c3c3c3c3c3c3c3c3", "Twin", true},
	{"d4d4d4d4d4d4d4d4d4d4d4d4", "Twin", false},
}

const (
	numRateLimited     = "+15550000429"
	numAllowanceUsed   = "+15550000430"
	numPlanRefused     = "+15550000403"
	numServerProblem   = "+15550000500"
	numValidation      = "+15550000400"
	numConnectionDrops = "+15550000999"
	idemInProgress     = "still-in-progress"
)

type mockRequest struct {
	method, path, rawURL string
	query                url.Values
	header               http.Header
	body                 js.Value
}

type mockMessage struct {
	to, message, deviceID string
	queued                bool
	polls                 int
}

type stored struct {
	fingerprint string
	status      int
	body        js.Value
}

type mockAPI struct {
	mu          sync.Mutex
	server      *httptest.Server
	apiBase     string
	requests    []mockRequest
	messages    map[string]*mockMessage
	received    []js.Value
	counter     int
	idempotency map[string]stored
	replays     int
}

func startMock(t *testing.T) *mockAPI {
	m := &mockAPI{messages: map[string]*mockMessage{}, idempotency: map[string]stored{}}
	now := time.Now()
	m.received = []js.Value{
		receivedText("e5e5e5e5e5e5e5e5e5e5e5e5", "+40711111111", "Earlier reply", now.Add(-3*time.Hour)),
		receivedText("f6f6f6f6f6f6f6f6f6f6f6f6", "+40722222222", "See you\nat 10", now.Add(-20*time.Minute)),
	}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	m.apiBase = m.server.URL + "/api/v1"
	t.Cleanup(m.server.Close)
	return m
}

func receivedText(id, sender, message string, at time.Time) js.Value {
	return js.NewObject().
		Set("id", js.StringValue(id)).
		Set("sender", js.StringValue(sender)).
		Set("message", js.StringValue(message)).
		Set("isEncrypted", js.BoolValue(false)).
		Set("receivedAt", js.StringValue(js.ISOString(float64(at.UnixMilli())))).
		Set("deviceId", js.StringValue(mockPhones[0].id)).
		Set("deviceName", js.StringValue("Desk phone"))
}

func (m *mockAPI) addReceived(id, sender, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.received = append(m.received, receivedText(id, sender, message, time.Now()))
}

func (m *mockAPI) lastRequest(path, method string) *mockRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.requests) - 1; i >= 0; i-- {
		if m.requests[i].path == path && m.requests[i].method == method {
			r := m.requests[i]
			return &r
		}
	}
	return nil
}

func (m *mockAPI) sendsSoFar() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.requests {
		if r.path == "/api/v1/sms/send" || r.path == "/api/v1/sms/send-2fa" {
			n++
		}
	}
	return n
}

func (m *mockAPI) textCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.messages)
}

func jsonBody(w http.ResponseWriter, status int, body js.Value, headers ...string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	for i := 0; i+1 < len(headers); i += 2 {
		w.Header().Set(headers[i], headers[i+1])
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, js.Stringify(body, ""))
}

func obj(pairs ...any) js.Value {
	o := js.NewObject()
	for i := 0; i+1 < len(pairs); i += 2 {
		var v js.Value
		switch x := pairs[i+1].(type) {
		case js.Value:
			v = x
		case string:
			v = js.StringValue(x)
		case int:
			v = js.IntValue(x)
		case bool:
			v = js.BoolValue(x)
		case []string:
			items := make([]js.Value, len(x))
			for k, s := range x {
				items[k] = js.StringValue(s)
			}
			v = js.ArrayValue(items)
		case nil:
			v = js.NullValue
		default:
			panic(fmt.Sprintf("obj: %T", x))
		}
		o.Set(pairs[i].(string), v)
	}
	return o
}

type mockAnswer struct {
	status  int
	body    js.Value
	headers []string
	drop    bool
}

func (m *mockAPI) sendAnswer(path string, body js.Value) mockAnswer {
	for _, k := range body.Keys() {
		switch k {
		case "to", "message", "deviceId", "defaultCountryCode":
		default:
			return mockAnswer{status: 400, body: obj("statusCode", 400, "message", []string{"property " + k + " should not exist"}, "error", "Bad Request")}
		}
	}
	to, message, deviceID := body.Get("to"), body.Get("message"), body.Get("deviceId")
	if !to.IsString() || !message.IsString() {
		return mockAnswer{status: 400, body: obj("statusCode", 400, "message", []string{"to must be a string", "message must be a string"}, "error", "Bad Request")}
	}
	switch to.Str() {
	case numRateLimited:
		return mockAnswer{status: 429, body: obj("statusCode", 429, "message", "ThrottlerException: Too Many Requests"), headers: []string{"Retry-After", "7"}}
	case numAllowanceUsed:
		return mockAnswer{status: 429, body: obj("statusCode", 429, "message", "You've sent today's 25 texts on the Free plan. More go out tomorrow.")}
	case numPlanRefused:
		return mockAnswer{status: 403, body: obj("statusCode", 403, "message", "Maximum privacy needs the Professional or Enterprise plan", "error", "Forbidden")}
	case numServerProblem:
		return mockAnswer{status: 500, body: obj("statusCode", 500, "message", "Internal server error")}
	case numValidation:
		return mockAnswer{status: 400, body: obj("statusCode", 400, "message", []string{"Phone number must contain 7-25 characters", "second problem"}, "error", "Bad Request")}
	case numConnectionDrops:
		return mockAnswer{drop: true}
	}
	if path == "/sms/send-2fa" && !containsSignInCode(message.Str()) {
		return mockAnswer{status: 400, body: obj("statusCode", 400, "message", "Invalid 2FA message: No valid verification code found", "error", "Bad Request")}
	}
	phone := mockPhones[0]
	if !deviceID.IsUndefined() {
		found := false
		for _, p := range mockPhones {
			if deviceID.StrictEquals(p.id) {
				phone, found = p, true
			}
		}
		if !found {
			return mockAnswer{status: 404, body: obj("statusCode", 404, "message", "Device not found", "error", "Not Found")}
		}
	}
	queued := !phone.online
	m.counter++
	id := fmt.Sprintf("%024x", m.counter)
	m.messages[id] = &mockMessage{to: to.Str(), message: message.Str(), deviceID: phone.id, queued: queued}
	data := obj("messageId", id, "branded", strings.Contains(message.Str(), "BRANDED"))
	if queued {
		data.Set("queued", js.BoolValue(true))
	}
	data.Set("remaining", obj("clean", 9, "total", "unlimited"))
	answer := obj("success", true, "messageId", id)
	if queued {
		answer.Set("queued", js.BoolValue(true))
	}
	answer.Set("data", data)
	return mockAnswer{status: 200, body: answer}
}

func (m *mockAPI) handle(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := js.UndefinedValue
	if len(raw) > 0 {
		if v, err := js.Parse(string(raw)); err == nil {
			body = v
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, mockRequest{method: r.Method, path: r.URL.Path, rawURL: r.URL.RequestURI(), query: r.URL.Query(), header: r.Header.Clone(), body: body})

	if strings.HasPrefix(r.URL.Path, "/redirect-base/") {
		w.Header().Set("Location", "https://example.com/")
		w.WriteHeader(302)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/html-base/") {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "<!doctype html><title>Some site</title>")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1")
	key := r.Header.Get("X-API-Key")
	if key == "" {
		jsonBody(w, 401, obj("statusCode", 401, "message", "API key is required.", "error", "Unauthorized"))
		return
	}
	if key != keyFull && key != keySendOnly {
		jsonBody(w, 401, obj("statusCode", 401, "message", "Invalid or expired API key", "error", "Unauthorized"))
		return
	}
	switch {
	case r.Method == "GET" && path == "/devices":
		var devices []js.Value
		online := 0
		for _, p := range mockPhones {
			seen := time.Now().Add(-2 * 24 * time.Hour)
			if p.online {
				seen = time.Now().Add(-30 * time.Second)
				online++
			}
			devices = append(devices, obj("id", p.id, "name", p.name, "isOnline", p.online, "lastSeenAt", js.ISOString(float64(seen.UnixMilli()))))
		}
		jsonBody(w, 200, obj("success", true, "data", obj("devices", js.ArrayValue(devices), "online", online, "total", len(devices))))
	case r.Method == "POST" && path == "/sms/send-bulk":
		jsonBody(w, 500, obj("statusCode", 500, "message", "The CLI must never call send-bulk"))
	case r.Method == "POST" && (path == "/sms/send" || path == "/sms/send-2fa"):
		m.handleSend(w, r, path, key, body)
	case r.Method == "GET" && strings.HasPrefix(path, "/sms/status/") && !strings.Contains(strings.TrimPrefix(path, "/sms/status/"), "/"):
		m.handleStatus(w, strings.TrimPrefix(path, "/sms/status/"))
	case r.Method == "GET" && path == "/sms/received":
		m.handleReceived(w, r, key)
	case r.Method == "GET" && path == "/account/usage":
		jsonBody(w, 200, obj("success", true, "data", obj(
			"plan", "free", "accountType", "standard",
			"usage", obj("smsClean", 3, "smsBranded", 0, "twoFaClean", 1, "smsCleanMonth", 40),
			"limits", obj("smsClean", 25, "twoFaClean", 20, "smsCleanPerMonth", 500, "smsBrandedPerDay", 25),
			"remaining", obj("smsClean", 22, "twoFaClean", 19, "smsCleanMonth", 460, "smsBranded", 25))))
	default:
		jsonBody(w, 404, obj("statusCode", 404, "message", "Cannot "+r.Method+" "+r.URL.Path, "error", "Not Found"))
	}
}

func (m *mockAPI) drop(w http.ResponseWriter) {
	if hj, ok := w.(http.Hijacker); ok {
		if conn, _, err := hj.Hijack(); err == nil {
			_ = conn.Close()
		}
	}
}

func (m *mockAPI) handleSend(w http.ResponseWriter, r *http.Request, path, key string, body js.Value) {
	idemKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idemKey == "" {
		a := m.sendAnswer(path, body)
		if a.drop {
			m.drop(w)
			return
		}
		jsonBody(w, a.status, a.body, a.headers...)
		return
	}
	if idemKey == idemInProgress {
		jsonBody(w, 409, obj("statusCode", 409, "error", "Conflict", "code", "IDEMPOTENCY_KEY_IN_PROGRESS",
			"message", "A request with this Idempotency-Key is still being processed. Retry in a moment to get its result."), "Retry-After", "1")
		return
	}
	scope := key + "\n" + idemKey
	fingerprint := path + "\n" + js.Stringify(body, "")
	if s, ok := m.idempotency[scope]; ok {
		if s.fingerprint != fingerprint {
			jsonBody(w, 422, obj("statusCode", 422, "error", "Unprocessable Entity", "code", "IDEMPOTENCY_KEY_REUSED",
				"message", "This Idempotency-Key was already used in the last 24 hours for a different request (another endpoint, recipient, text, phone or country code). Use a new key for a new text."))
			return
		}
		m.replays++
		jsonBody(w, s.status, s.body, "Idempotent-Replayed", "true")
		return
	}
	a := m.sendAnswer(path, body)
	if a.drop {
		m.idempotency[scope] = stored{fingerprint, 500, obj("statusCode", 500, "message", "Internal server error")}
		m.drop(w)
		return
	}
	if a.status < 300 || a.status >= 500 {
		m.idempotency[scope] = stored{fingerprint, a.status, a.body}
	}
	jsonBody(w, a.status, a.body, a.headers...)
}

func (m *mockAPI) handleStatus(w http.ResponseWriter, id string) {
	rec, ok := m.messages[id]
	if !ok {
		jsonBody(w, 404, obj("statusCode", 404, "message", "Message not found", "error", "Not Found"))
		return
	}
	rec.polls++
	if strings.Contains(rec.message, "STATUS403") {
		jsonBody(w, 403, obj("statusCode", 403, "error", "Forbidden", "code", "PERMISSION_REQUIRED", "permission", "sms:read",
			"message", "This API key lacks the 'sms:read' permission"))
		return
	}
	if strings.Contains(rec.message, "STATUS429ONCE") && rec.polls == 1 {
		jsonBody(w, 429, obj("statusCode", 429, "message", "ThrottlerException: Too Many Requests"), "Retry-After", "1")
		return
	}
	data := obj("id", id, "phoneNumber", rec.to, "sentAt", nil, "deliveredAt", nil, "errorMessage", nil)
	switch {
	case rec.queued || rec.polls < 2:
		data.Set("status", js.StringValue("pending")).Set("deliveryStatus", js.StringValue("pending"))
	case strings.Contains(rec.message, "FAILME"):
		data.Set("errorMessage", js.StringValue("Generic failure (the SIM refused it)")).Set("status", js.StringValue("failed")).Set("deliveryStatus", js.StringValue("failed"))
	case strings.Contains(rec.message, "UNCONFIRMED"):
		data.Set("errorMessage", js.StringValue("Not confirmed by the phone - the SMS may have been sent; it is not resent automatically")).
			Set("status", js.StringValue("failed")).Set("unconfirmed", js.BoolValue(true)).Set("deliveryStatus", js.StringValue("unconfirmed"))
	default:
		data.Set("sentAt", js.StringValue(js.ISOString(float64(time.Now().UnixMilli())))).Set("status", js.StringValue("sent")).Set("deliveryStatus", js.StringValue("sent"))
	}
	jsonBody(w, 200, obj("success", true, "data", data))
}

func (m *mockAPI) handleReceived(w http.ResponseWriter, r *http.Request, key string) {
	if key == keySendOnly {
		jsonBody(w, 403, obj("statusCode", 403, "error", "Forbidden", "code", "PERMISSION_REQUIRED", "permission", "sms:inbox",
			"message", "This API key may not read received texts. Allow it in the dashboard (API Keys → 'Allow reading received messages') or create a key with that permission."))
		return
	}
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		fmt.Sscan(v, &limit)
	}
	limit = min(max(limit, 1), 100)
	digits := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, s)
	}
	var rows []js.Value
	for _, row := range m.received {
		if since := q.Get("since"); since != "" && js.DateOf(row.Get("receivedAt")) < js.ParseDate(since) {
			continue
		}
		if from := q.Get("phone"); from != "" && !strings.Contains(digits(row.Get("sender").Str()), digits(from)) {
			continue
		}
		if id := q.Get("deviceId"); id != "" && !row.Get("deviceId").StrictEquals(id) {
			continue
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return js.DateOf(rows[i].Get("receivedAt")) > js.DateOf(rows[j].Get("receivedAt"))
	})
	page := rows
	if len(page) > limit {
		page = page[:limit]
	}
	if page == nil {
		page = []js.Value{}
	}
	jsonBody(w, 200, obj("success", true, "data", obj("messages", js.ArrayValue(page), "total", len(rows), "page", 1, "limit", limit, "hasMore", len(rows) > limit)))
}
