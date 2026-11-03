package cli

import (
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

const (
	watchOverlapMs = 5 * 60000
	maxRemembered  = 5000
)

// receivedLine is one text on one line: time, sender, the phone that got it, the words (line breaks as " / ").
func receivedLine(m js.Value) string {
	words := "[encrypted]"
	if !m.Get("isEncrypted").Truthy() && !m.Get("message").IsNull() {
		words = joinLines(m.Get("message").String())
	}
	phone := ""
	if name := m.Get("deviceName"); name.Truthy() {
		phone = "  (" + name.String() + ")"
	}
	return formatLocal(m.Get("receivedAt")) + "  " + m.Get("sender").String() + phone + "  " + words
}

// joinLines is .replace(/\s*\r?\n\s*/g, " / "): every run of white space that holds a line feed becomes " / ".
func joinLines(s string) string {
	cps := js.CodePoints(s)
	var b strings.Builder
	for i := 0; i < len(cps); {
		if !js.IsSpace(cps[i]) {
			b.WriteString(string(cps[i]))
			i++
			continue
		}
		j, newline := i, false
		for j < len(cps) && js.IsSpace(cps[j]) {
			if cps[j] == '\n' {
				newline = true
			}
			j++
		}
		if newline {
			b.WriteString(" / ")
		} else {
			for _, r := range cps[i:j] {
				b.WriteString(string(r))
			}
		}
		i = j
	}
	return b.String()
}

// oldestFirst sorts texts by receivedAt, keeping the server's order for equal times (Array.prototype.sort is stable).
func oldestFirst(messages []js.Value) []js.Value {
	sorted := append([]js.Value(nil), messages...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return js.DateOf(sorted[i].Get("receivedAt"))-js.DateOf(sorted[j].Get("receivedAt")) < 0
	})
	return sorted
}

// idKey identifies a text's id for the set of texts already shown (ids of different types never match).
func idKey(v js.Value) string { return string(rune('0'+v.Kind())) + js.Stringify(v, "") }

func received(c *runContext, args []string) (int, error) {
	if len(args) > 0 {
		return 0, usageError(`"smstunnel received" takes no arguments; use --since, --from, --phone or --limit.`)
	}
	since, sinceSet := 0.0, false
	if v, ok := c.opts.str("since"); ok {
		t, valid := parseSince(v, c.nowMs())
		if !valid {
			return 0, usageError("--since takes a date (2026-11-03 or 2026-11-03T08:00:00Z) or a span back from now (30m, 2h, 1d).")
		}
		since, sinceSet = t, true
	}
	limit := 20
	if v, ok := c.opts.str("limit"); ok {
		n := -1.0
		if v != "" && strings.Trim(v, "0123456789") == "" {
			n = js.ToNumber(v)
		}
		if n < 1 || n > 100 {
			return 0, usageError("--limit takes a number from 1 to 100.")
		}
		limit = int(n)
	}
	watch := c.opts.flag("watch")
	intervalSeconds := 10.0
	if v, ok := c.opts.str("interval"); ok {
		parsed, valid := parseSeconds(v)
		if !valid || parsed < 3 {
			return 0, usageError("--interval takes a number of seconds, at least 3.")
		}
		intervalSeconds = parsed
		if !watch {
			return 0, usageError("--interval goes with --watch.")
		}
	}
	from := ""
	if v, ok := c.opts.str("from"); ok {
		from = js.Trim(v)
	}

	key, err := requireKey(c)
	if err != nil {
		return 0, err
	}
	client, err := c.client(key.key)
	if err != nil {
		return 0, err
	}
	deviceID := js.UndefinedValue
	if v, ok := c.opts.str("phone"); ok {
		deviceID, err = resolvePhone(c, client, key.source, v, opReceived)
		if err != nil {
			return 0, err
		}
	}
	fetch := func(after float64, set bool) (*answer, error) {
		return client.getReceived(receivedQuery{limit: limit, since: after, sinceSet: set, phone: from, deviceID: deviceID})
	}

	lastPoll := c.nowMs()
	first, err := fetch(since, sinceSet)
	if err != nil {
		return 0, c.describe(err, opReceived, key.source, false)
	}
	data := first.body.Get("data")
	if data.IsNullish() {
		data = js.NewObject().
			Set("messages", js.ArrayValue([]js.Value{})).
			Set("total", js.IntValue(0)).
			Set("page", js.IntValue(1)).
			Set("limit", js.IntValue(limit)).
			Set("hasMore", js.BoolValue(false))
	}
	if data.Get("messages").Kind() != js.Array {
		return 0, &jsError{name: "TypeError", message: "data.messages is not iterable"}
	}
	messages := oldestFirst(data.Get("messages").Items())

	if !watch {
		if c.json {
			c.data(data)
			return exitOK, nil
		}
		if len(messages) == 0 {
			sinceText := ""
			if sinceSet {
				sinceText = " since " + formatLocalTime(since)
			}
			c.line("No received texts" + sinceText + `. Your phone syncs received texts only after "Sync received texts" is turned on in the SMSTunnel app, and only those that arrive after that.`)
			return exitOK, nil
		}
		for _, m := range messages {
			c.line(receivedLine(m))
		}
		if data.Get("hasMore").Truthy() {
			c.note("Showing the latest " + js.NumberToString(float64(len(messages))) + " of " + data.Get("total").String() + ". Use --limit (up to 100) or --since to see others.")
		}
		return exitOK, nil
	}

	// --watch: what is there, then each new text as it arrives, until Ctrl+C.
	seen := map[string]bool{}
	var order []string // the ids in the order they were first shown, to forget the oldest
	show := func(m js.Value) {
		k := idKey(m.Get("id"))
		if !seen[k] {
			seen[k] = true
			order = append(order, k)
			if len(order) > maxRemembered {
				delete(seen, order[0])
				order = order[1:]
			}
		}
		if c.json {
			c.jsonLine(m)
		} else {
			c.line(receivedLine(m))
		}
	}
	for _, m := range messages {
		show(m)
	}
	if !c.json {
		c.note("Watching for new texts every " + js.NumberToString(intervalSeconds) + " s… (Ctrl+C stops)")
	}

	in := startInterrupt(c)
	defer in.close()
	interval := c.pollInterval(intervalSeconds * 1000)
	delay := interval
	for {
		in.sleep(delay)
		if in.interrupted {
			return exitOK, nil
		}
		pollStart := c.nowMs()
		// A margin back in time, so a text synced late is not missed; ids already shown are skipped.
		floor := 0.0
		if sinceSet {
			floor = since
		}
		after := js.TimeClip(maxNumber(floor, lastPoll-watchOverlapMs))
		page, err := fetch(after, true)
		if err == nil {
			lastPoll = pollStart
			delay = interval
			items := page.body.Get("data").Get("messages")
			var fresh []js.Value
			for _, m := range items.Items() {
				if !seen[idKey(m.Get("id"))] {
					fresh = append(fresh, m)
				}
			}
			for _, m := range oldestFirst(fresh) {
				show(m)
			}
			continue
		}
		var apiErr *apiError
		if !errors.As(err, &apiErr) {
			return 0, err
		}
		next, stop := watchErrorDelay(c, apiErr, key.source, interval)
		if stop != nil {
			return 0, stop
		}
		delay = next
	}
}

// watchErrorDelay is how long to wait after a failed poll, or the error that ends watching (401, 403…).
func watchErrorDelay(c *runContext, e *apiError, source keySource, interval float64) (float64, error) {
	if e.statusCode() == 429 {
		wait := 30.0
		if e.retryAfter.Kind() == js.Number {
			wait = e.retryAfter.Num()
		}
		return math.Max(interval, wait*1000), nil
	}
	reason := describeAPIError(e, errorContext{operation: opReceived, keySource: source, apiBase: c.apiBase})
	if e.kind == "network" || e.kind == "timeout" || e.statusCode() >= 500 {
		next := math.Min(60000, interval*2)
		c.note(reason.Message + " Trying again in " + js.NumberToString(js.Round(next/1000)) + " s.")
		return next, nil
	}
	return 0, reason
}
