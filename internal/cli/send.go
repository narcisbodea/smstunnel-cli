package cli

import (
	"errors"
	"math"
	"strings"
	"time"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

const alreadySent = "Already handed over with this --idempotency-key: SMSTunnel gave back its answer to the first request and did not send the text again."

const defaultTimeoutSeconds = 120

// outcomeOf is a final state of a status, or "" while the text still waits for the phone.
func outcomeOf(s js.Value) string {
	if u := s.Get("unconfirmed"); u.Kind() == js.Bool && u.BoolVal() || s.Get("deliveryStatus").StrictEquals("unconfirmed") {
		return "unconfirmed"
	}
	switch st := s.Get("status"); {
	case st.StrictEquals("sent"), st.StrictEquals("delivered"):
		return "sent"
	case st.StrictEquals("failed"):
		return "failed"
	}
	return ""
}

// messageFrom is the text: the words joined with spaces, or stdin for "-" (or no words and stdin not a terminal).
// "\r\n" becomes "\n" and the newlines at the end go.
func messageFrom(c *runContext, words []string) (string, error) {
	var text string
	switch {
	case len(words) == 1 && words[0] == "-", len(words) == 0 && !c.env.Stdin.IsTerminal():
		t, err := readAllStdin(c.env.Stdin)
		if err != nil {
			return "", err
		}
		text = t
	default:
		text = strings.Join(words, " ")
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.TrimRight(text, "\n"), nil
}

func send(c *runContext, args []string, code bool) (int, error) {
	op := opSend
	if code {
		op = opCode
	}
	number := ""
	var words []string
	if len(args) > 0 {
		number, words = args[0], args[1:]
	}
	recipient, problem := checkRecipient(number)
	if problem != "" {
		return 0, usageError(problem)
	}
	message, err := messageFrom(c, words)
	if err != nil {
		return 0, err
	}
	if js.Trim(message) == "" {
		if code {
			return 0, usageError(`Write the text with the code, like: smstunnel code +40712345678 "Your sign-in code is 482913"`)
		}
		return 0, usageError(`Write the message, like: smstunnel send +40712345678 "See you at 10"`)
	}
	max, what := maxTextLength, "a text"
	if code {
		max, what = maxCodeTextLength, "a sign-in code text"
	}
	if count := js.CodePointCount(message); count > max {
		return 0, usageError("The text has " + js.NumberToString(float64(count)) + " characters; " + what + " can have up to " + js.NumberToString(float64(max)) + ".")
	}
	if code && !containsSignInCode(message) {
		return 0, usageError(`The text must contain the code: 4 to 10 digits standing on their own (482913) or two groups of 3 joined by a dash (482-913). ` +
			`Texts without a code go through "smstunnel send".`)
	}
	country := ""
	if v, ok := c.opts.str("country"); ok {
		checked, problem := checkCountryCode(v)
		if problem != "" {
			return 0, usageError(problem)
		}
		country = checked
	}
	wait := c.opts.flag("wait")
	timeoutSeconds := float64(defaultTimeoutSeconds)
	if v, ok := c.opts.str("timeout"); ok {
		parsed, valid := parseSeconds(v)
		if !valid {
			return 0, usageError("--timeout takes a number of seconds, like --timeout 60.")
		}
		timeoutSeconds = parsed
		if !wait {
			return 0, usageError("--timeout goes with --wait.")
		}
	}
	idempotencyKey := ""
	if v, ok := c.opts.str("idempotency-key"); ok {
		checked, problem := checkIdempotencyKey(v)
		if problem != "" {
			return 0, usageError(problem)
		}
		idempotencyKey = checked
	}

	key, err := requireKey(c)
	if err != nil {
		return 0, err
	}
	client, err := c.client(key.key)
	if err != nil {
		return 0, err
	}
	device := js.UndefinedValue
	hasDevice := false
	if v, ok := c.opts.str("phone"); ok {
		device, err = resolvePhone(c, client, key.source, v, op)
		if err != nil {
			return 0, err
		}
		hasDevice = true
	}

	// Only the fields SMSTunnel takes: it refuses any other with 400.
	body := js.NewObject().Set("to", js.StringValue(recipient)).Set("message", js.StringValue(message))
	if hasDevice {
		body.Set("deviceId", device)
	}
	if country != "" {
		body.Set("defaultCountryCode", js.StringValue(country))
	}
	res, err := client.sendText(code, body, idempotencyKey)
	if err != nil {
		return 0, c.describe(err, op, key.source, idempotencyKey != "")
	}
	response, data := res.body, res.body.Get("data")
	messageID := response.Get("messageId")
	if messageID.IsNullish() {
		messageID = data.Get("messageId")
	}
	if !messageID.Truthy() {
		return 0, &CliError{Message: "SMSTunnel took the text but sent back no message id. Check the Messages page in your dashboard (" +
			dashboardURL + ") before you send it again.", ExitCode: exitUnexpected}
	}
	queuedValue := response.Get("queued")
	if queuedValue.IsNullish() {
		queuedValue = data.Get("queued")
	}
	queued := queuedValue.Truthy()
	branded := data.Get("branded").Truthy()
	_, _, parts := smsParts(message)
	remaining := data.Get("remaining")
	if remaining.IsNullish() {
		remaining = js.NullValue
	}
	id := messageID.String()
	summary := func(result string, status js.Value) js.Value {
		return js.NewObject().
			Set("messageId", messageID).
			Set("queued", js.BoolValue(queued)).
			Set("branded", js.BoolValue(branded)).
			Set("replayed", js.BoolValue(res.replayed)).
			Set("parts", js.IntValue(parts)).
			Set("remaining", remaining).
			Set("result", js.StringValue(result)).
			Set("status", status)
	}

	if !wait {
		if c.json {
			result := "handed-over"
			if queued {
				result = "queued"
			}
			c.data(summary(result, js.NullValue))
			return exitOK, nil
		}
		if res.replayed {
			c.line(alreadySent)
		}
		if queued {
			c.line("Queued: none of your phones is online right now. The text goes out when your phone is back online (within 3 days).")
		} else {
			c.line("Handed to your phone, which sends it from your number.")
		}
		partsText := ""
		if parts > 1 {
			partsText = " (" + js.NumberToString(float64(parts)) + " parts)"
		}
		c.line("Message id: " + id + partsText + ". Check it with: smstunnel status " + id)
		if branded {
			c.line("Free plan: a short line was added to this text.")
		}
		return exitOK, nil
	}

	timeoutText := js.NumberToString(timeoutSeconds)
	if !c.json {
		if res.replayed {
			c.note(alreadySent)
		}
		if queued {
			c.note("Queued (message id " + id + "): none of your phones is online right now. Waiting up to " + timeoutText + " s… (Ctrl+C stops waiting; the text still goes out)")
		} else {
			c.note("Handed to your phone (message id " + id + "). Waiting up to " + timeoutText + " s for it to be sent… (Ctrl+C stops waiting)")
		}
	}

	outcome, last, err := waitForPhone(c, client, key.source, id, messageID, timeoutSeconds)
	if err != nil {
		return 0, err
	}
	if c.json {
		status := last
		if status.IsUndefined() {
			status = js.NullValue
		}
		c.data(summary(outcome, status))
	} else {
		switch outcome {
		case "sent":
			at := ""
			if sentAt := last.Get("sentAt"); sentAt.Truthy() {
				at = " at " + formatLocal(sentAt)
			}
			c.line("Sent by your phone" + at + ". Message id: " + id)
			if branded {
				c.line("Free plan: a short line was added to this text.")
			}
		case "unconfirmed":
			c.line("Not confirmed by your phone: it handed the text to the SIM, so it was most likely sent. Don't send it again. Message id: " + id)
		case "failed":
			c.note("Not sent: " + errorMessageOr(last) + ". Message id: " + id)
		case "still-waiting":
			offline := ""
			if queued {
				offline = " (it is offline)"
			}
			c.note("Still waiting for your phone after " + timeoutText + " s" + offline + ". " +
				"The text goes out when your phone is back online; don't send it again. Check it later with: smstunnel status " + id)
		case "interrupted":
			c.note("Stopped waiting. The text is still on its way; check it with: smstunnel status " + id)
		}
	}
	switch outcome {
	case "sent", "unconfirmed":
		return exitOK, nil
	case "failed":
		return exitTextFailed, nil
	case "still-waiting":
		return exitStillWaiting, nil
	}
	return exitInterrupted, nil
}

// interrupt tells whether Ctrl+C was pressed, and sleeps unless it was.
type interrupt struct {
	ch          <-chan struct{}
	stop        func()
	interrupted bool
}

func startInterrupt(c *runContext) *interrupt {
	in := &interrupt{}
	if c.env.Interrupts != nil {
		in.ch, in.stop = c.env.Interrupts()
	}
	return in
}

func (in *interrupt) close() {
	if in.stop != nil {
		in.stop()
	}
}

// sleep waits ms milliseconds, or less when Ctrl+C comes (or came before).
func (in *interrupt) sleep(ms float64) {
	in.poll()
	if in.interrupted || !(ms > 0) {
		return
	}
	if ms < 1 {
		ms = 1 // setTimeout waits at least a millisecond
	}
	timer := time.NewTimer(time.Duration(math.Min(ms, float64(math.MaxInt64/2)) * float64(time.Millisecond)))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-in.ch:
		in.interrupted = true
	}
}

// poll notes a Ctrl+C pressed while a request was running.
func (in *interrupt) poll() {
	select {
	case <-in.ch:
		in.interrupted = true
	default:
	}
}

// waitForPhone polls the status until the phone sent the text, it failed, the time is up or Ctrl+C.
func waitForPhone(c *runContext, client *apiClient, source keySource, id string, messageID js.Value, timeoutSeconds float64) (string, js.Value, error) {
	in := startInterrupt(c)
	defer in.close()
	deadline := c.nowMs() + timeoutSeconds*1000
	interval := c.pollInterval(2000)
	last := js.UndefinedValue
	failures := 0
	left := func() float64 { return math.Max(0, deadline-c.nowMs()) }
	for {
		in.sleep(math.Min(interval, left()))
		if in.interrupted {
			return "interrupted", last, nil
		}
		res, err := client.getStatus(id)
		if err == nil {
			failures = 0
			if data := res.body.Get("data"); data.Truthy() {
				last = data
				if outcome := outcomeOf(last); outcome != "" {
					return outcome, last, nil
				}
			}
		} else {
			var apiErr *apiError
			if !errors.As(err, &apiErr) {
				return "", last, err
			}
			switch {
			case apiErr.statusCode() == 429:
				wait := 5.0
				if apiErr.retryAfter.Kind() == js.Number {
					wait = apiErr.retryAfter.Num()
				}
				in.sleep(math.Min(wait*1000, left()))
			case apiErr.kind == "network" || apiErr.kind == "timeout" || apiErr.statusCode() >= 500:
				failures++
				if failures >= 3 {
					reason := describeAPIError(apiErr, errorContext{operation: opStatus, keySource: source, apiBase: c.apiBase})
					reason.details.messageID = messageID
					return "", last, &CliError{Message: reason.Message + " The text was handed over (message id " + id + "); check it later with: smstunnel status " + id,
						ExitCode: reason.ExitCode, details: reason.details}
				}
			default:
				reason := describeAPIError(apiErr, errorContext{operation: opStatus, keySource: source, apiBase: c.apiBase})
				reason.details.messageID = messageID
				return "", last, &CliError{Message: reason.Message + " The text itself was handed over (message id " + id + "): don't send it again.",
					ExitCode: reason.ExitCode, details: reason.details}
			}
		}
		in.poll()
		if c.nowMs() >= deadline {
			return "still-waiting", last, nil
		}
	}
}
