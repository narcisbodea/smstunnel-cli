package cli

import (
	"strings"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

// phonesTable: ID, NAME, STATUS and LAST SEEN, padded to the widest cell (in UTF-16 code units, like the Node
// version), two spaces between columns, the last one not padded.
func phonesTable(devices []js.Value, nowMs float64) []string {
	header := []string{"ID", "NAME", "STATUS", "LAST SEEN"}
	rows := make([][]string, len(devices))
	for i, d := range devices {
		online := d.Get("isOnline").Truthy()
		state, seen := "offline", formatAgo(d.Get("lastSeenAt"), nowMs)
		if online {
			state, seen = "online", "now"
		}
		rows[i] = []string{d.Get("id").String(), d.Get("name").String(), state, seen}
	}
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

func phones(c *runContext, args []string) (int, error) {
	if len(args) > 0 {
		return 0, usageError(`"smstunnel phones" takes no arguments.`)
	}
	key, err := requireKey(c)
	if err != nil {
		return 0, err
	}
	client, err := c.client(key.key)
	if err != nil {
		return 0, err
	}
	res, err := client.getDevices()
	if err != nil {
		return 0, c.describe(err, opPhones, key.source, false)
	}
	data := res.body.Get("data")
	if data.IsNullish() {
		data = js.NewObject().Set("devices", js.ArrayValue([]js.Value{})).Set("online", js.IntValue(0)).Set("total", js.IntValue(0))
	}
	if c.json {
		c.data(data)
		return exitOK, nil
	}
	devicesValue := data.Get("devices")
	if devicesValue.Kind() != js.Array {
		return 0, &jsError{name: "TypeError", message: "Cannot read properties of undefined (reading 'length')"}
	}
	devices := devicesValue.Items()
	if len(devices) == 0 {
		c.line("No phones are linked to your account yet. Install the SMSTunnel app on your Android phone and sign in: " + playURL)
		return exitOK, nil
	}
	for _, line := range phonesTable(devices, c.nowMs()) {
		c.line(line)
	}
	online := 0
	for _, d := range devices {
		if d.Get("isOnline").Truthy() {
			online++
		}
	}
	c.line("")
	c.line(js.NumberToString(float64(online)) + " of " + js.NumberToString(float64(len(devices))) + " online. Send from one with --phone <id or name>.")
	return exitOK, nil
}

// describeStatus is one sentence on where a text is now.
func describeStatus(s js.Value) string {
	if s.Get("unconfirmed").Kind() == js.Bool && s.Get("unconfirmed").BoolVal() || s.Get("deliveryStatus").StrictEquals("unconfirmed") {
		return "Not confirmed by your phone: it handed the text to the SIM, so it was most likely sent. Don't send it again."
	}
	switch status := s.Get("status"); {
	case status.StrictEquals("sent"):
		if at := s.Get("sentAt"); at.Truthy() {
			return "Sent by your phone at " + formatLocal(at) + "."
		}
		return "Sent by your phone."
	case status.StrictEquals("delivered"):
		if at := s.Get("deliveredAt"); at.Truthy() {
			return "Delivered at " + formatLocal(at) + "."
		}
		return "Delivered."
	case status.StrictEquals("failed"):
		return "Not sent: " + errorMessageOr(s) + "."
	case status.StrictEquals("pending"):
		return "Waiting for your phone to send it. If your phone is offline, the text goes out when it is back online (within 3 days)."
	default:
		return "Status: " + status.String() + "."
	}
}

// errorMessageOr is status.errorMessage || "your phone could not send it".
func errorMessageOr(s js.Value) string {
	if m := s.Get("errorMessage"); m.Truthy() {
		return m.String()
	}
	return "your phone could not send it"
}

func status(c *runContext, args []string) (int, error) {
	if len(args) != 1 {
		return 0, usageError("Give one message id: smstunnel status <message-id>")
	}
	id := js.Trim(args[0])
	if !isHex(id, 24) {
		return 0, usageError(`A message id has 24 characters, 0-9 and a-f: the one "smstunnel send" printed.`)
	}
	key, err := requireKey(c)
	if err != nil {
		return 0, err
	}
	client, err := c.client(key.key)
	if err != nil {
		return 0, err
	}
	res, err := client.getStatus(id)
	if err != nil {
		return 0, c.describe(err, opStatus, key.source, false)
	}
	data := res.body.Get("data")
	if !data.Truthy() {
		return 0, usageError("SMSTunnel sent back no status.")
	}
	if c.json {
		c.data(data)
		return exitOK, nil
	}
	c.line(describeStatus(data))
	if to := data.Get("phoneNumber"); to.Truthy() {
		c.line("To: " + to.String())
	}
	shown := data.Get("id")
	if shown.IsNullish() {
		shown = js.StringValue(id)
	}
	c.line("Message id: " + shown.String())
	return exitOK, nil
}

// usageLine is "<label>: <used> used, <left> left[ (of <limit>)]", or nothing when used and left are both missing.
func usageLine(label string, used, left, limit js.Value) (string, bool) {
	if used.IsUndefined() && left.IsUndefined() {
		return "", false
	}
	shown := func(v js.Value) string {
		if v.IsNullish() {
			return "-"
		}
		return v.String()
	}
	of := ""
	if !limit.IsNullish() && !limit.StrictEquals("unlimited") {
		of = " (of " + shown(limit) + ")"
	}
	return label + ": " + shown(used) + " used, " + shown(left) + " left" + of, true
}

func usageLines(data js.Value) []string {
	orEmpty := func(v js.Value) js.Value {
		if v.IsNullish() {
			return js.NewObject()
		}
		return v
	}
	usageV, limits, remaining := orEmpty(data.Get("usage")), orEmpty(data.Get("limits")), orEmpty(data.Get("remaining"))
	plan := "-"
	if p := data.Get("plan"); !p.IsNullish() {
		plan = p.String()
	}
	first := "Plan: " + plan
	if e := data.Get("enterprise"); e.Truthy() {
		first += " (shared with " + e.Get("name").String() + ")"
	}
	lines := []string{first}
	add := func(text string, ok bool) {
		if ok {
			lines = append(lines, text)
		}
	}
	add(usageLine("Texts today", usageV.Get("smsClean"), remaining.Get("smsClean"), limits.Get("smsClean")))
	if m := limits.Get("smsCleanPerMonth"); !m.IsUndefined() && !m.StrictEquals("unlimited") {
		add(usageLine("Texts this month", usageV.Get("smsCleanMonth"), remaining.Get("smsCleanMonth"), m))
	}
	if b := limits.Get("smsBrandedPerDay"); !b.IsUndefined() && !(b.Kind() == js.Number && b.Num() == 0) {
		add(usageLine("Texts with the free line today", usageV.Get("smsBranded"), remaining.Get("smsBranded"), b))
	}
	add(usageLine("Sign-in codes today", usageV.Get("twoFaClean"), remaining.Get("twoFaClean"), limits.Get("twoFaClean")))
	return lines
}

func usage(c *runContext, args []string) (int, error) {
	if len(args) > 0 {
		return 0, usageError(`"smstunnel usage" takes no arguments.`)
	}
	key, err := requireKey(c)
	if err != nil {
		return 0, err
	}
	client, err := c.client(key.key)
	if err != nil {
		return 0, err
	}
	res, err := client.getUsage()
	if err != nil {
		return 0, c.describe(err, opUsage, key.source, false)
	}
	data := res.body.Get("data")
	if data.IsNullish() {
		data = js.NewObject()
	}
	if c.json {
		c.data(data)
		return exitOK, nil
	}
	for _, line := range usageLines(data) {
		c.line(line)
	}
	return exitOK, nil
}
