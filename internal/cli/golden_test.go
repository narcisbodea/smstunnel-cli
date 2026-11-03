package cli

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

// The golden files hold what the Node.js version answers, function by function, for a corpus of inputs
// (test/parity/golden.mjs writes them). This version must answer the same.

func loadGolden(t *testing.T) map[string]js.Value {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "golden", "node-*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden files: %v", err)
	}
	out := map[string]js.Value{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		v, err := js.Parse(string(data))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		out[f] = v
	}
	return out
}

// inZone runs fn with local time in the golden file's time zone.
func inZone(t *testing.T, golden js.Value, fn func()) {
	t.Helper()
	loc, err := time.LoadLocation(golden.Get("zone").Str())
	if err != nil {
		t.Fatalf("time zone %s: %v", golden.Get("zone").Str(), err)
	}
	saved := js.Location
	js.Location = loc
	defer func() { js.Location = saved }()
	fn()
}

func goldenNow(golden js.Value) float64 { return js.ParseDate(golden.Get("now").Str()) }

func numberFromGolden(v js.Value) float64 {
	if v.IsString() {
		switch v.Str() {
		case "NaN":
			return math.NaN()
		case "-0":
			return math.Copysign(0, -1)
		case "Infinity":
			return math.Inf(1)
		case "-Infinity":
			return math.Inf(-1)
		}
	}
	return v.Num()
}

func sameNumber(a, b float64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.IsNaN(a) && math.IsNaN(b)
	}
	return a == b && math.Signbit(a) == math.Signbit(b)
}

func timeText(t float64, ok bool) string {
	if !ok {
		return "undefined"
	}
	if !js.IsValid(t) {
		return "Invalid Date"
	}
	return js.ISOString(t)
}

func joinChars(v js.Value) string {
	var b strings.Builder
	for _, c := range v.Items() {
		b.WriteString(c.Str())
	}
	return b.String()
}

func TestGolden(t *testing.T) {
	for file, golden := range loadGolden(t) {
		golden := golden
		t.Run(filepath.Base(file), func(t *testing.T) {
			inZone(t, golden, func() { checkGolden(t, golden) })
		})
	}
}

func checkGolden(t *testing.T, g js.Value) {
	now := goldenNow(g)
	count := 0
	fail := func(section string, input js.Value, got, want any) {
		t.Helper()
		t.Errorf("%s %s: got %#v, want %#v", section, js.Stringify(input, ""), got, want)
	}

	for _, c := range g.Get("urls").Items() {
		count++
		got, err := normalizeAPIBase(c.Get("input").Str())
		if ok := c.Get("ok"); !ok.IsUndefined() {
			if err != nil || got != ok.Str() {
				fail("url", c.Get("input"), [2]any{got, err}, ok.Str())
			}
			continue
		}
		cliErr, isCli := err.(*CliError)
		if !isCli || cliErr.Message != c.Get("error").Str() || float64(cliErr.ExitCode) != c.Get("exitCode").Num() {
			fail("url", c.Get("input"), [2]any{got, err}, c.Get("error").Str())
		}
	}
	for _, c := range g.Get("dates").Items() {
		count++
		got, want := js.ParseDate(c.Get("input").Str()), numberFromGolden(c.Get("value"))
		if !sameNumber(got, want) {
			fail("Date.parse", c.Get("input"), timeText(got, true), timeText(want, true))
		}
	}
	for _, c := range g.Get("since").Items() {
		count++
		got := timeText(parseSince(c.Get("input").Str(), now))
		if want := c.Get("value").Str(); got != want {
			fail("--since", c.Get("input"), got, want)
		}
	}
	for _, c := range g.Get("phones").Items() {
		count++
		value, message := checkRecipient(c.Get("input").Str())
		if c.Get("ok").BoolVal() {
			if message != "" || value != c.Get("value").Str() {
				fail("number", c.Get("input"), message, c.Get("value").Str())
			}
		} else if message != c.Get("message").Str() {
			fail("number", c.Get("input"), message, c.Get("message").Str())
		}
	}
	for _, c := range g.Get("countries").Items() {
		count++
		value, message := checkCountryCode(c.Get("input").Str())
		if c.Get("ok").BoolVal() {
			if message != "" || value != c.Get("value").Str() {
				fail("country", c.Get("input"), message, c.Get("value").Str())
			}
		} else if message != c.Get("message").Str() {
			fail("country", c.Get("input"), message, c.Get("message").Str())
		}
	}
	for _, c := range g.Get("codes").Items() {
		count++
		if got := containsSignInCode(c.Get("input").Str()); got != c.Get("value").BoolVal() {
			fail("code", c.Get("input"), got, c.Get("value").BoolVal())
		}
	}
	for _, c := range g.Get("texts").Items() {
		count++
		encoding, characters, parts := smsParts(c.Get("input").Str())
		if encoding != c.Get("encoding").Str() || float64(characters) != c.Get("characters").Num() || float64(parts) != c.Get("parts").Num() {
			fail("parts", c.Get("input"), []any{encoding, characters, parts}, js.Stringify(c, ""))
		}
	}
	for _, c := range g.Get("idems").Items() {
		count++
		value, message := checkIdempotencyKey(c.Get("input").Str())
		if c.Get("ok").BoolVal() {
			if message != "" || value != c.Get("value").Str() {
				fail("idempotency key", c.Get("input"), message, c.Get("value").Str())
			}
		} else if message != c.Get("message").Str() {
			fail("idempotency key", c.Get("input"), message, c.Get("message").Str())
		}
	}
	for _, c := range g.Get("seconds").Items() {
		count++
		got, ok := parseSeconds(c.Get("input").Str())
		want := c.Get("value")
		if want.IsNull() == ok || (ok && got != want.Num()) {
			fail("seconds", c.Get("input"), [2]any{got, ok}, js.Stringify(want, ""))
		}
	}
	for _, c := range g.Get("polls").Items() {
		count++
		ctx := &runContext{env: &Env{Getenv: func(name string) string {
			if name == "SMSTUNNEL_POLL_INTERVAL_MS" {
				return c.Get("input").Str()
			}
			return ""
		}}}
		if got, want := ctx.pollInterval(2000), numberFromGolden(c.Get("value")); !sameNumber(got, want) {
			fail("poll interval", c.Get("input"), got, want)
		}
	}
	for _, c := range g.Get("lowers").Items() {
		count++
		if got := js.ToLower(c.Get("input").Str()); got != c.Get("value").Str() {
			fail("toLowerCase", c.Get("input"), got, c.Get("value").Str())
		}
	}
	for _, c := range g.Get("trims").Items() {
		count++
		if got := js.Trim(c.Get("input").Str()); got != c.Get("value").Str() {
			fail("trim", c.Get("input"), got, c.Get("value").Str())
		}
	}
	for _, c := range g.Get("jsons").Items() {
		count++
		v, err := js.Parse(c.Get("input").Str())
		if c.Get("error").BoolVal() {
			if err == nil {
				fail("JSON.parse", c.Get("input"), js.Stringify(v, ""), "a syntax error")
			}
			continue
		}
		if err != nil {
			fail("JSON.parse", c.Get("input"), err, "a value")
			continue
		}
		if got := js.Stringify(v, "  "); got != c.Get("pretty").Str() {
			fail("JSON.stringify(v, null, 2)", c.Get("input"), got, c.Get("pretty").Str())
		}
		if got := js.Stringify(v, ""); got != c.Get("compact").Str() {
			fail("JSON.stringify", c.Get("input"), got, c.Get("compact").Str())
		}
		if s := c.Get("string"); s.IsString() && v.String() != s.Str() {
			fail("String", c.Get("input"), v.String(), s.Str())
		}
	}
	nowTime := time.UnixMilli(int64(now))
	for _, c := range g.Get("retries").Items() {
		count++
		got, ok := parseRetryAfter(c.Get("input").Str(), nowTime)
		want := c.Get("value")
		if want.IsNull() == ok || (ok && got != want.Num()) {
			fail("Retry-After", c.Get("input"), [2]any{got, ok}, js.Stringify(want, ""))
		}
	}
	for _, c := range g.Get("errorBodies").Items() {
		count++
		body := c.Get("body")
		if body.StrictEquals("__undefined__") {
			body = js.UndefinedValue
		}
		if got := errorMessageFrom(body, c.Get("text").Str(), int(c.Get("status").Num())); got != c.Get("value").Str() {
			fail("errorMessageFrom", c, got, c.Get("value").Str())
		}
	}
	for _, c := range g.Get("statuses").Items() {
		count++
		if got := describeStatus(c.Get("input")); got != c.Get("value").Str() {
			fail("describeStatus", c.Get("input"), got, c.Get("value").Str())
		}
		want := ""
		if o := c.Get("outcome"); o.IsString() {
			want = o.Str()
		}
		if got := outcomeOf(c.Get("input")); got != want {
			fail("outcomeOf", c.Get("input"), got, want)
		}
	}
	for _, c := range g.Get("usages").Items() {
		count++
		got := strings.Join(usageLines(c.Get("input")), "\n")
		var want []string
		for _, l := range c.Get("value").Items() {
			want = append(want, l.Str())
		}
		if got != strings.Join(want, "\n") {
			fail("usageLines", c.Get("input"), got, strings.Join(want, "\n"))
		}
	}
	for _, c := range g.Get("received").Items() {
		count++
		if got := receivedLine(c.Get("input")); got != c.Get("value").Str() {
			fail("receivedLine", c.Get("input"), got, c.Get("value").Str())
		}
	}
	for _, c := range g.Get("tables").Items() {
		count++
		got := strings.Join(phonesTable(c.Get("input").Items(), now), "\n")
		var want []string
		for _, l := range c.Get("value").Items() {
			want = append(want, l.Str())
		}
		if got != strings.Join(want, "\n") {
			fail("phonesTable", c.Get("input"), got, strings.Join(want, "\n"))
		}
	}
	for _, c := range g.Get("locals").Items() {
		count++
		if got := formatLocal(c.Get("input")); got != c.Get("value").Str() {
			fail("formatLocal", c.Get("input"), got, c.Get("value").Str())
		}
	}
	for _, c := range g.Get("agos").Items() {
		count++
		if got := formatAgo(c.Get("input"), now); got != c.Get("value").Str() {
			fail("formatAgo", c.Get("input"), got, c.Get("value").Str())
		}
	}
	for _, c := range g.Get("masks").Items() {
		count++
		if got, want := maskKey(joinChars(c.Get("chars"))), joinChars(c.Get("valueChars")); got != want {
			t.Errorf("maskKey: got %q, want %q", got, want)
		}
	}
	for _, c := range g.Get("forms").Items() {
		count++
		if got := js.FormEncode(c.Get("input").Str()); got != c.Get("value").Str() {
			fail("URLSearchParams", c.Get("input"), got, c.Get("value").Str())
		}
	}
	apiErrors := g.Get("apiErrors")
	contexts, messages := apiErrors.Get("contexts").Items(), apiErrors.Get("messages").Items()
	for _, r := range apiErrors.Get("results").Items() {
		e := apiErrorFromGolden(r.Get("error"))
		for i, cell := range r.Get("row").Items() {
			count++
			ctx := contexts[i]
			ec := errorContext{
				operation:      operation(ctx.Get("operation").Str()),
				keySource:      keySource(ctx.Get("keySource").Str()),
				apiBase:        "https://smstunnel.io/api/v1",
				idempotencyKey: ctx.Get("idempotencyKey").BoolVal(),
			}
			got := describeAPIError(e, ec)
			pair := cell.Items()
			want := messages[int(pair[0].Num())].Str()
			if got.Message != want || float64(got.ExitCode) != pair[1].Num() {
				t.Errorf("describeApiError %s in %s: got %q (%d), want %q (%v)", js.Stringify(r.Get("error"), ""), js.Stringify(ctx, ""), got.Message, got.ExitCode, want, pair[1].Num())
			}
			details := js.NewObject().Set("status", got.details.status).Set("code", got.details.code).
				Set("permission", got.details.permission).Set("retryAfter", got.details.retryAfter)
			if js.Stringify(details, "") != r.Get("details").Str() {
				t.Errorf("details of %s: got %s, want %s", js.Stringify(r.Get("error"), ""), js.Stringify(details, ""), r.Get("details").Str())
			}
		}
	}
	if count < 2500 {
		t.Errorf("only %d golden cases were checked", count)
	}
	t.Logf("%d cases in %s", count, g.Get("zone").Str())
}

func apiErrorFromGolden(v js.Value) *apiError {
	e := &apiError{
		kind:           v.Get("kind").Str(),
		message:        v.Get("message").Str(),
		status:         v.Get("status"),
		code:           v.Get("code"),
		permission:     v.Get("permission"),
		retryAfter:     v.Get("retryAfter"),
		networkCode:    v.Get("networkCode").Str(),
		mayHaveArrived: v.Get("mayHaveArrived").BoolVal(),
		replayed:       v.Get("replayed").BoolVal(),
	}
	return e
}
