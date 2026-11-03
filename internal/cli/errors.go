package cli

import (
	"strconv"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

// details of a CliError, written in its JSON form in this order when set: status, code, permission, retryAfter,
// messageId. Undefined values are left out.
type details struct {
	status     js.Value
	code       js.Value
	permission js.Value
	retryAfter js.Value
	messageID  js.Value
}

// CliError is an error the user should read, with the exit code the process ends with.
type CliError struct {
	Message  string
	ExitCode int
	details  details
}

func (e *CliError) Error() string { return e.Message }

func usageError(message string) *CliError { return &CliError{Message: message, ExitCode: exitUsage} }

// jsError is what the Node version ends with when JavaScript itself throws (a bug of that version that this one
// reproduces where it is cheap to): "Unexpected error: RangeError: Invalid time value", exit code 1.
type jsError struct{ name, message string }

func (e *jsError) Error() string { return e.message }

// apiError is a call SMSTunnel refused, or that never got an answer.
type apiError struct {
	kind           string // "http", "network", "timeout", "redirect" or "invalid-response"
	message        string // for "http": SMSTunnel's words (errorMessageFrom)
	status         js.Value
	code           js.Value
	permission     js.Value
	retryAfter     js.Value
	networkCode    string
	mayHaveArrived bool
	replayed       bool
}

func (e *apiError) Error() string { return e.message }

func (e *apiError) statusCode() int {
	if e.status.Kind() == js.Number {
		return int(e.status.Num())
	}
	return 0
}

type operation string

const (
	opLogin    operation = "login"
	opPhones   operation = "phones"
	opSend     operation = "send"
	opCode     operation = "code"
	opStatus   operation = "status"
	opReceived operation = "received"
	opUsage    operation = "usage"
)

type keySource string

const (
	keyFromEnv    keySource = "env"
	keyFromStored keySource = "stored"
	keyFromTyped  keySource = "typed"
)

type errorContext struct {
	operation      operation
	keySource      keySource
	apiBase        string
	idempotencyKey bool // the send carried --idempotency-key
}

func sends(op operation) bool { return op == opSend || op == opCode }

func hostOf(apiBase string) string {
	u, err := js.ParseURL(apiBase)
	if err != nil {
		return apiBase
	}
	return u.HostPort()
}

const dashboardKeyHint = "Create a key in your dashboard (" + dashboardURL + " → Apps → Add an app)"

const sameKeyAgain = "Run the same command again with the same --idempotency-key: SMSTunnel will not send the text twice."

func seconds(n float64) string {
	text := js.NumberToString(n)
	if n == 1 {
		return text + " second"
	}
	return text + " seconds"
}

// describeAPIError is what a refused or failed API call means for the person at the keyboard, and the exit code.
func describeAPIError(e *apiError, c errorContext) *CliError {
	d := details{status: e.status, code: e.code, permission: e.permission, retryAfter: e.retryAfter}
	fail := func(message string, exit int) *CliError {
		return &CliError{Message: message, ExitCode: exit, details: d}
	}
	host := hostOf(c.apiBase)

	switch e.kind {
	case "network", "timeout":
		if sends(c.operation) && e.mayHaveArrived {
			next := "Check the Messages page in your dashboard (" + dashboardURL + ") before you send it again."
			if c.idempotencyKey {
				next = sameKeyAgain
			}
			return fail("The connection to SMSTunnel broke after the text was handed over, so it may have gone out. "+next, exitUnavailable)
		}
		why := e.networkCode
		if e.kind == "timeout" {
			why = "no answer in time"
		}
		return fail("Could not reach SMSTunnel at "+host+" ("+why+"). Check your internet connection and try again.", exitUnavailable)
	case "redirect":
		return fail(host+" answered with a redirect (HTTP "+e.status.String()+"). Check the API address (--api-base).", exitUnavailable)
	case "invalid-response":
		return fail("Unexpected answer from "+host+" (HTTP "+e.status.String()+"): it is not the SMSTunnel API. Check the API address (--api-base).", exitUnavailable)
	}

	status := e.statusCode()
	said := e.message
	switch {
	case status == 400:
		return fail("SMSTunnel refused the request: "+said, exitUsage)
	case status == 422 && e.code.StrictEquals("IDEMPOTENCY_KEY_REUSED"):
		return fail(`This --idempotency-key was already used in the last 24 hours for a different text (another number, message, phone or `+
			`country code, or "code" instead of "send"). Nothing was sent. Use a new key for a new text.`, exitUsage)
	case status == 409 && e.code.StrictEquals("IDEMPOTENCY_KEY_IN_PROGRESS"):
		n := 1.0
		if e.retryAfter.Kind() == js.Number && e.retryAfter.Num() > n {
			n = e.retryAfter.Num()
		}
		return fail("SMSTunnel is still handling an earlier request with this --idempotency-key. Run the same command again in "+
			seconds(n)+" to get its answer; it will not send a second text.", exitRateLimited)
	case status == 401:
		from := " (the saved key)"
		switch c.keySource {
		case keyFromEnv:
			from = " (the key in SMSTUNNEL_API_KEY)"
		case keyFromTyped:
			from = ""
		}
		next := `, then run "smstunnel login" again.`
		if c.operation == opLogin || c.keySource == keyFromEnv {
			next = "."
		}
		return fail("SMSTunnel refused the API key"+from+": it is wrong, deactivated or deleted. "+dashboardKeyHint+next, exitAuth)
	case status == 403:
		if e.code.StrictEquals("PERMISSION_REQUIRED") {
			switch {
			case e.permission.StrictEquals("sms:inbox"):
				return fail("This API key may not read received texts. In your dashboard ("+dashboardURL+" → Apps), choose "+
					`"Allow reading received texts" for this key, or add an app with "Can also read received texts" ticked. `+
					`Your phone also needs "Sync received texts" turned on in the SMSTunnel app; only texts that arrive after that are synced.`, exitForbidden)
			case e.permission.StrictEquals("sms:send"):
				return fail(`This API key may not send texts (it lacks the "sms:send" permission). `+dashboardKeyHint+".", exitForbidden)
			case e.permission.StrictEquals("sms:read"):
				return fail(`This API key may not check whether texts went out (it lacks the "sms:read" permission). `+dashboardKeyHint+".", exitForbidden)
			}
			permission := "needed"
			if !e.permission.IsNullish() {
				permission = e.permission.String()
			}
			return fail(`This API key lacks the "`+permission+`" permission. `+said, exitForbidden)
		}
		return fail("SMSTunnel refused this: "+said, exitForbidden)
	case status == 404:
		if c.operation == opStatus {
			return fail("No text with that id for this key.", exitNotFound)
		}
		if sends(c.operation) || c.operation == opReceived {
			return fail(said+`. List your phones and their ids with "smstunnel phones".`, exitNotFound)
		}
		return fail("Not found: "+said, exitNotFound)
	case status == 429:
		if e.retryAfter.Kind() == js.Number {
			n := e.retryAfter.Num()
			if !(n >= 1) {
				n = maxNumber(1, n)
			}
			what := "Too many requests in a minute."
			if sends(c.operation) {
				what = "Too many texts in a minute: SMSTunnel takes up to 30 send requests a minute."
			}
			return fail(what+" Try again in "+seconds(n)+".", exitRateLimited)
		}
		// No Retry-After: the plan's allowance for today (or this month) is used up.
		text := said
		if said == "" || isHTTPStatusText(said) {
			text = "Your plan's allowance for today is used up."
		}
		return fail(text, exitRateLimited)
	case status >= 500:
		if sends(c.operation) {
			first := "SMSTunnel had a problem (HTTP " + strconv.Itoa(status) + "). "
			if e.replayed {
				first = "SMSTunnel had a problem (HTTP " + strconv.Itoa(status) + ") with the first request that used this --idempotency-key, and gives that same answer for 24 hours. "
			}
			then := "The text may or may not have been taken: check the Messages page in your dashboard (" + dashboardURL + ") before you send it again."
			if c.idempotencyKey {
				then = "The text may or may not have been taken: check the Messages page in your dashboard (" + dashboardURL + "). Running the command again with the same key gets this answer again; use a new key only once you know the text did not go out."
			}
			return fail(first+then, exitUnavailable)
		}
		return fail("SMSTunnel had a problem (HTTP "+strconv.Itoa(status)+"). Try again in a little while.", exitUnavailable)
	}
	return fail("SMSTunnel refused the request (HTTP "+strconv.Itoa(status)+"): "+said, exitUnexpected)
}

// maxNumber is Math.max(a, b) (NaN wins).
func maxNumber(a, b float64) float64 {
	if b != b || a != a {
		return js.NaN
	}
	if b > a {
		return b
	}
	return a
}

// isHTTPStatusText matches /^HTTP \d+$/.
func isHTTPStatusText(s string) bool {
	if len(s) < 6 || s[:5] != "HTTP " {
		return false
	}
	for i := 5; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
