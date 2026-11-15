package cli

import (
	"errors"
	"strings"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

// Scheduled texts: "smstunnel send --at" hands a text to SMSTunnel now, which sends it at its time with the same
// key; "smstunnel scheduled" lists the texts the key scheduled and cancels one. The Node.js version (1.0.0) has
// neither (COMPATIBILITY.md).

// reportScheduled prints what SMSTunnel answered to send --at: {scheduled, scheduledId, sendAt, data}.
func reportScheduled(c *runContext, res *answer, message string, at float64) (int, error) {
	response, data := res.body, res.body.Get("data")
	id := response.Get("scheduledId")
	if id.IsNullish() {
		id = data.Get("id")
	}
	if !id.Truthy() {
		return 0, &CliError{Message: `SMSTunnel took the text but sent back no scheduled id. Check "smstunnel scheduled" and the Messages page in your dashboard (` +
			dashboardURL + ") before you send it again.", ExitCode: exitUnexpected}
	}
	sendAt := response.Get("sendAt")
	if sendAt.IsNullish() {
		sendAt = data.Get("sendAt")
	}
	when := at
	if t := js.DateOf(sendAt); sendAt.Truthy() && js.IsValid(t) {
		when = t
	} else {
		sendAt = js.StringValue(js.ISOString(at))
	}
	_, _, parts := smsParts(message)
	if c.json {
		scheduledText := data
		if scheduledText.Kind() != js.Object {
			scheduledText = js.NullValue
		}
		c.data(js.NewObject().
			Set("scheduledId", id).
			Set("sendAt", sendAt).
			Set("replayed", js.BoolValue(res.replayed)).
			Set("parts", js.IntValue(parts)).
			Set("result", js.StringValue("scheduled")).
			Set("scheduled", scheduledText))
		return exitOK, nil
	}
	if res.replayed {
		c.line(alreadyScheduled)
	}
	c.line("Scheduled for " + formatWhen(when) + ": SMSTunnel hands it to your phone then, which sends it from your number.")
	partsText := ""
	if parts > 1 {
		partsText = " (" + js.NumberToString(float64(parts)) + " parts)"
	}
	c.line("Scheduled id: " + id.String() + partsText + ". Cancel it with: smstunnel scheduled cancel " + id.String())
	return exitOK, nil
}

func scheduled(c *runContext, args []string) (int, error) {
	if len(args) > 0 {
		if args[0] != "cancel" {
			return 0, usageError(`"smstunnel scheduled" lists your scheduled texts; to cancel one: smstunnel scheduled cancel <id>`)
		}
		return cancelScheduled(c, args[1:])
	}
	done := c.opts.flag("done")
	limit, page := 0, 0
	if v, ok := c.opts.str("limit"); ok {
		n, valid := wholeNumber(v, 100)
		if !valid {
			return 0, usageError("--limit takes a number from 1 to 100.")
		}
		limit = n
	}
	if v, ok := c.opts.str("page"); ok {
		n, valid := wholeNumber(v, 1000)
		if !valid {
			return 0, usageError("--page takes a number from 1 to 1000.")
		}
		page = n
	}

	key, err := requireKey(c)
	if err != nil {
		return 0, err
	}
	client, err := c.client(key.key)
	if err != nil {
		return 0, err
	}
	res, err := client.getScheduled(scheduledQuery{done: done, limit: limit, page: page})
	if err != nil {
		return 0, c.describe(err, opScheduled, key.source, false)
	}
	data := res.body.Get("data")
	if data.IsNullish() {
		data = js.NewObject().Set("items", js.ArrayValue([]js.Value{})).Set("total", js.IntValue(0))
	}
	if c.json {
		c.data(data)
		return exitOK, nil
	}
	itemsValue := data.Get("items")
	if itemsValue.Kind() != js.Array {
		return 0, &CliError{Message: "SMSTunnel sent back no list of scheduled texts.", ExitCode: exitUnexpected}
	}
	items := itemsValue.Items()
	if len(items) == 0 {
		if total := data.Get("total"); total.Kind() == js.Number && total.Num() > 0 {
			shown := data.Get("page")
			if shown.Kind() != js.Number {
				shown = js.IntValue(max(page, 1))
			}
			c.line("Page " + shown.String() + " is past the last one: " + total.String() + " in all. Use a smaller --page.")
			return exitOK, nil
		}
		if done {
			c.line("No scheduled texts went out, failed or were canceled in the last 30 days.")
		} else {
			c.line(`No texts are waiting. Schedule one with: smstunnel send <number> <message…> --at "YYYY-MM-DD HH:MM"`)
		}
		return exitOK, nil
	}
	for _, line := range scheduledTable(items, done) {
		c.line(line)
	}
	c.line("")
	total := data.Get("total")
	if total.Kind() != js.Number {
		total = js.IntValue(len(items))
	}
	if done {
		c.line(total.String() + " in the last 30 days. Check a sent one with: smstunnel status <message id>")
	} else {
		summary := total.String() + " waiting."
		if waiting, allowed := data.Get("waiting"), data.Get("max"); waiting.Kind() == js.Number && allowed.Kind() == js.Number {
			summary += " Your account has " + waiting.String() + " of the " + allowed.String() + " texts your plan lets wait at once."
		}
		c.line(summary + " Cancel one with: smstunnel scheduled cancel <id>")
	}
	shownPage, shownLimit := data.Get("page"), data.Get("limit")
	if shownPage.Kind() == js.Number && shownLimit.Kind() == js.Number && shownPage.Num()*shownLimit.Num() < total.Num() {
		c.note("Showing " + js.NumberToString(float64(len(items))) + " of " + total.String() + ". See the next ones with --page " +
			js.NumberToString(shownPage.Num()+1) + " (and --limit, up to 100).")
	}
	return exitOK, nil
}

// wholeNumber reads --limit and --page: digits only, from 1 to most.
func wholeNumber(v string, most int) (int, bool) {
	if v == "" || strings.Trim(v, "0123456789") != "" {
		return 0, false
	}
	n := js.ToNumber(v)
	if n < 1 || n > float64(most) {
		return 0, false
	}
	return int(n), true
}

// scheduledTable: waiting texts as ID, SEND AT, TO and TEXT; done ones as SEND AT, STATUS, MESSAGE ID, TO and TEXT.
// Times are this computer's; line breaks in a text show as " / ".
func scheduledTable(items []js.Value, done bool) []string {
	header := []string{"ID", "SEND AT", "TO", "TEXT"}
	if done {
		header = []string{"SEND AT", "STATUS", "MESSAGE ID", "TO", "TEXT"}
	}
	cell := func(v js.Value) string {
		if v.IsNullish() {
			return "-"
		}
		return v.String()
	}
	rows := make([][]string, len(items))
	for i, item := range items {
		sendAt, to, text := formatLocal(item.Get("sendAt")), cell(item.Get("to")), ""
		if m := item.Get("message"); !m.IsNullish() {
			text = joinLines(m.String())
		}
		if !done {
			rows[i] = []string{cell(item.Get("id")), sendAt, to, text}
			continue
		}
		state := cell(item.Get("status"))
		if reason := item.Get("error"); reason.Truthy() {
			state += " (" + reason.String() + ")"
		}
		messageID := "-"
		if id := item.Get("messageId"); id.Truthy() {
			messageID = id.String()
		}
		rows[i] = []string{sendAt, state, messageID, to, text}
	}
	return formatTable(header, rows)
}

// formatTable pads every column but the last to its widest cell (in UTF-16 code units, like phonesTable), with two
// spaces between columns.
func formatTable(header []string, rows [][]string) []string {
	widths := make([]int, len(header))
	for col, title := range header {
		widths[col] = js.Length(title)
		for _, row := range rows {
			if n := js.Length(row[col]); n > widths[col] {
				widths[col] = n
			}
		}
	}
	format := func(cells []string) string {
		out := make([]string, len(cells))
		for col, cell := range cells {
			if col == len(cells)-1 {
				out[col] = cell
			} else {
				out[col] = js.PadEnd(cell, widths[col])
			}
		}
		return strings.Join(out, "  ")
	}
	lines := []string{format(header)}
	for _, row := range rows {
		lines = append(lines, format(row))
	}
	return lines
}

func cancelScheduled(c *runContext, args []string) (int, error) {
	for _, name := range []string{"done", "limit", "page"} {
		if c.opts.has(name) {
			return 0, usageError(`--` + name + ` goes with the list: "smstunnel scheduled cancel" takes only the id.`)
		}
	}
	if len(args) != 1 {
		return 0, usageError("Give one scheduled text's id: smstunnel scheduled cancel <id>")
	}
	id := js.Trim(args[0])
	if !isHex(id, 24) {
		return 0, usageError(`A scheduled text's id has 24 characters, 0-9 and a-f: the one "smstunnel send --at" printed, or see "smstunnel scheduled".`)
	}
	key, err := requireKey(c)
	if err != nil {
		return 0, err
	}
	client, err := c.client(key.key)
	if err != nil {
		return 0, err
	}
	res, err := client.cancelScheduled(id)
	if err != nil {
		var apiErr *apiError
		if errors.As(err, &apiErr) && apiErr.statusCode() == 409 && apiErr.code.StrictEquals("SCHEDULE_NOT_EDITABLE") {
			return tooLateToCancel(c, client, key.source, id, apiErr)
		}
		return 0, c.describe(err, opCancel, key.source, false)
	}
	data := res.body.Get("data")
	if data.Kind() != js.Object {
		data = js.NewObject().Set("id", js.StringValue(id)).Set("status", js.StringValue("canceled"))
	}
	if c.json {
		c.data(data)
		return exitOK, nil
	}
	c.line("Canceled: " + scheduledTextName(data) + " won't go out.")
	return exitOK, nil
}

// tooLateToCancel says why SMSTunnel would not cancel a text (409), after looking it up. A text that was already
// canceled ends with 0, so running the same cancel twice is fine; one that went out or failed is exit 2.
func tooLateToCancel(c *runContext, client *apiClient, source keySource, id string, refusal *apiError) (int, error) {
	reason := describeAPIError(refusal, errorContext{operation: opCancel, keySource: source, apiBase: c.apiBase})
	res, err := client.getScheduledText(id)
	if err != nil {
		return 0, reason
	}
	data := res.body.Get("data")
	if data.Kind() != js.Object {
		return 0, reason
	}
	name := scheduledTextName(data)
	switch status := data.Get("status"); {
	case status.StrictEquals("canceled"):
		if c.json {
			c.data(data)
			return exitOK, nil
		}
		c.line("Already canceled: " + name + " won't go out.")
		return exitOK, nil
	case status.StrictEquals("sent"):
		reason.Message = "Too late to cancel: " + name + " already went out."
		if messageID := data.Get("messageId"); messageID.Truthy() {
			reason.Message += " Check it with: smstunnel status " + messageID.String()
			reason.details.messageID = messageID
		}
	case status.StrictEquals("failed"):
		why := ""
		if e := data.Get("error"); e.Truthy() {
			why = " (" + e.String() + ")"
		}
		reason.Message = "Nothing to cancel: " + name + " could not be sent at its time" + why + ", so it did not go out."
	case status.StrictEquals("scheduled"):
		reason.Message = "Too late to cancel: SMSTunnel is handing " + name + " to your phone right now. " +
			`See how it went with: smstunnel scheduled --done`
	}
	return 0, reason
}

// scheduledTextName is "the text to +40712345678 for 2026-11-16 09:00 (2026-11-16 07:00 UTC)", as far as the
// answer says.
func scheduledTextName(data js.Value) string {
	name := "the text"
	if to := data.Get("to"); to.Truthy() {
		name += " to " + to.String()
	}
	if t := js.DateOf(data.Get("sendAt")); data.Get("sendAt").Truthy() && js.IsValid(t) {
		name += " for " + formatWhen(t)
	}
	return name
}
