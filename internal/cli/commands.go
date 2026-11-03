package cli

import (
	"errors"
	"strings"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

// requireKey is the key to call SMSTunnel with, or the exit-3 error saying how to get one.
func requireKey(c *runContext) (*loadedKey, error) {
	loaded, err := loadKey(c.env)
	if err != nil {
		return nil, err
	}
	if loaded == nil {
		return nil, &CliError{
			Message:  `No API key yet. Run "smstunnel login" and paste a key from your dashboard (` + dashboardURL + ` → Apps → Add an app), or set SMSTUNNEL_API_KEY.`,
			ExitCode: exitAuth,
		}
	}
	return loaded, nil
}

// describe turns a refused or failed call into the message for people, with its exit code; other errors pass.
func (c *runContext) describe(err error, op operation, source keySource, idempotencyKey bool) error {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return describeAPIError(apiErr, errorContext{operation: op, keySource: source, apiBase: c.apiBase, idempotencyKey: idempotencyKey})
	}
	return err
}

// resolvePhone reads --phone: an id from "smstunnel phones" (or the app's id), or the phone's exact name.
func resolvePhone(c *runContext, client *apiClient, source keySource, value string, op operation) (js.Value, error) {
	wanted := js.Trim(value)
	if isHex(wanted, 24) || isUUID(wanted) {
		return js.StringValue(wanted), nil
	}
	res, err := client.getDevices()
	if err != nil {
		return js.UndefinedValue, c.describe(err, op, source, false)
	}
	devices := devicesOf(res.body.Get("data"))
	var matches []js.Value
	for _, d := range devices {
		if d.Get("name").StrictEquals(wanted) {
			matches = append(matches, d)
		}
	}
	if len(matches) == 0 {
		lower := js.ToLower(wanted)
		for _, d := range devices {
			name := d.Get("name")
			if !name.IsString() {
				return js.UndefinedValue, &jsError{name: "TypeError", message: "device.name.toLowerCase is not a function"}
			}
			if js.ToLower(name.Str()) == lower {
				matches = append(matches, d)
			}
		}
	}
	switch {
	case len(matches) == 1:
		return matches[0].Get("id"), nil
	case len(matches) > 1:
		return js.UndefinedValue, usageError(`Several of your phones are called "` + wanted + `". Use the id instead (see "smstunnel phones").`)
	case len(devices) == 0:
		return js.UndefinedValue, usageError("No phones are linked to your account yet.")
	}
	names := make([]string, len(devices))
	for i, d := range devices {
		names[i] = `"` + d.Get("name").String() + `"`
	}
	return js.UndefinedValue, usageError(`No phone called "` + wanted + `". Your phones: ` + strings.Join(names, ", ") + `. "smstunnel phones" shows their ids.`)
}

// devicesOf is response.data?.devices ?? [].
func devicesOf(data js.Value) []js.Value {
	return data.Get("devices").Items()
}

func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, group := range strings.Split(s, "-") {
		if !isHex(group, []int{8, 4, 4, 4, 12}[min(i, 4)]) || i > 4 {
			return false
		}
	}
	return strings.Count(s, "-") == 4
}

// maskKey shows a key without giving it away: sk_live_01…cdef, or sk_… for a short one.
func maskKey(key string) string {
	units := js.UTF16(key)
	if len(units) <= 14 {
		return js.FromUTF16(units[:min(3, len(units))]) + "…"
	}
	return js.FromUTF16(units[:10]) + "…" + js.FromUTF16(units[len(units)-4:])
}

func login(c *runContext, args []string) (int, error) {
	if len(args) > 0 {
		return 0, usageError(`Don't put the key on the command line: it would stay in your shell history. Run "smstunnel login" and paste it when asked, or pipe it in: echo "$KEY" | smstunnel login`)
	}
	if js.Trim(c.env.Getenv("SMSTUNNEL_API_KEY")) != "" {
		c.note("Note: SMSTUNNEL_API_KEY is set, so it keeps being used instead of the key you save now.")
	}
	if c.apiBase != defaultAPIBase {
		c.note("Signing in at " + c.apiBase)
	}
	key, err := readSecret(c, "Paste your SMSTunnel API key ("+dashboardURL+" → Apps → Add an app) and press Enter: ")
	if err != nil {
		return 0, err
	}
	if key == "" {
		return 0, usageError("No key was given.")
	}
	if !keyPattern(key) {
		return 0, usageError("That does not look like an SMSTunnel API key (they look like sk_live_…).")
	}
	// Checked before it is saved: a key SMSTunnel refuses is never stored.
	client, err := c.client(key)
	if err != nil {
		return 0, err
	}
	res, err := client.getDevices()
	if err != nil {
		return 0, c.describe(err, opLogin, keyFromTyped, false)
	}
	devicesValue := res.body.Get("data").Get("devices")
	if devicesValue.IsNullish() {
		devicesValue = js.ArrayValue([]js.Value{})
	}
	apiBaseToSave := ""
	if c.apiBase != defaultAPIBase {
		apiBaseToSave = c.apiBase
	}
	saved, err := saveKey(c.env, key, apiBaseToSave)
	if err != nil {
		return 0, err
	}
	if c.json {
		c.data(js.NewObject().
			Set("signedIn", js.BoolValue(true)).
			Set("key", js.StringValue(maskKey(key))).
			Set("keyStore", js.StringValue(saved.kind)).
			Set("savedIn", js.StringValue(saved.label)).
			Set("apiBase", js.StringValue(c.apiBase)).
			Set("phones", devicesValue))
	} else {
		c.line("Signed in with " + maskKey(key) + ". The key is saved in " + saved.label + ".")
		devices := devicesValue.Items()
		empty := len(devices) == 0
		switch {
		case devicesValue.Kind() == js.String && devicesValue.Str() == "":
		case devicesValue.Kind() != js.Array:
			return 0, &jsError{name: "TypeError", message: "devices.map is not a function"}
		}
		if empty {
			c.line("No phones are linked yet: install the SMSTunnel app on your Android phone and sign in (" + playURL + ").")
		} else {
			parts := make([]string, len(devices))
			for i, d := range devices {
				state := "offline"
				if d.Get("isOnline").Truthy() {
					state = "online"
				}
				parts[i] = d.Get("name").String() + " (" + state + ")"
			}
			c.line("Your phones: " + strings.Join(parts, ", "))
		}
	}
	if saved.kind == storeFile {
		reason := saved.fallbackReason
		if reason == "" {
			reason = "no system keychain was found"
		}
		c.note("Warning: " + reason + ", so the key is saved in a file that only your user account can read (" + saved.label +
			`). Anyone who can sign in as you can read it; "smstunnel logout" removes it.`)
	}
	return exitOK, nil
}

func logout(c *runContext, args []string) (int, error) {
	if len(args) > 0 {
		return 0, usageError(`"smstunnel logout" takes no arguments.`)
	}
	removed, err := removeKey(c.env)
	if err != nil {
		return 0, err
	}
	if c.json {
		labels := make([]js.Value, len(removed))
		for i, l := range removed {
			labels[i] = js.StringValue(l)
		}
		c.data(js.NewObject().Set("signedOut", js.BoolValue(true)).Set("removedFrom", js.ArrayValue(labels)))
	} else if len(removed) > 0 {
		c.line("Signed out: the key was removed from " + strings.Join(removed, " and ") + ". " +
			"The key itself keeps working until you deactivate or delete it in your dashboard (Apps).")
	} else {
		c.line("There was no saved key on this computer.")
	}
	if js.Trim(c.env.Getenv("SMSTUNNEL_API_KEY")) != "" {
		c.note("Note: SMSTUNNEL_API_KEY is still set in your environment; unset it too.")
	}
	return exitOK, nil
}
