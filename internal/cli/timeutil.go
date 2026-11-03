package cli

import (
	"strconv"
	"strings"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

var unitMs = map[byte]float64{'s': 1000, 'm': 60000, 'h': 3600000, 'd': 86400000, 'w': 604800000}

// parseSince reads --since: "<n><unit>" (s, m, h, d, w) back from now, or a date or time that starts with
// YYYY-MM-DD (read by Date.parse). ok is false when it is neither; a span too long gives an invalid date (NaN),
// as in the Node version.
func parseSince(input string, nowMs float64) (float64, bool) {
	value := js.Trim(input)
	if amount, unit, ok := spanOf(value); ok {
		return js.TimeClip(nowMs - js.ToNumber(amount)*unitMs[unit]), true
	}
	if !startsWithDate(value) {
		return 0, false
	}
	parsed := js.ParseDate(value)
	if !js.IsValid(parsed) {
		return 0, false
	}
	return parsed, true
}

// spanOf matches /^(\d+)\s*([smhdw])$/i.
func spanOf(value string) (string, byte, bool) {
	i := 0
	for i < len(value) && value[i] >= '0' && value[i] <= '9' {
		i++
	}
	if i == 0 {
		return "", 0, false
	}
	digits := value[:i]
	cps := js.CodePoints(value[i:])
	for len(cps) > 1 && js.IsSpace(cps[0]) {
		cps = cps[1:]
	}
	if len(cps) != 1 {
		return "", 0, false
	}
	unit := cps[0]
	if unit >= 'A' && unit <= 'Z' {
		unit += 32
	}
	if _, ok := unitMs[byte(unit)]; !ok || unit > 'z' {
		return "", 0, false
	}
	return digits, byte(unit), true
}

// startsWithDate matches /^\d{4}-\d{2}-\d{2}/.
func startsWithDate(v string) bool {
	if len(v) < 10 {
		return false
	}
	for i, c := range []byte(v[:10]) {
		if i == 4 || i == 7 {
			if c != '-' {
				return false
			}
		} else if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func pad2(n int) string {
	if n < 10 && n >= 0 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// formatLocal is "2026-11-03 09:41" in this computer's time zone; "-" for nothing; a value that is not a date
// prints as it is.
func formatLocal(v js.Value) string {
	if !v.Truthy() {
		return "-"
	}
	t := js.DateOf(v)
	if !js.IsValid(t) {
		return v.String()
	}
	return formatLocalTime(t)
}

func formatLocalTime(t float64) string {
	if !js.IsValid(t) {
		return "Invalid Date"
	}
	p := js.LocalParts(t)
	return strconv.FormatInt(p.Year, 10) + "-" + pad2(p.Month) + "-" + pad2(p.Day) + " " + pad2(p.Hour) + ":" + pad2(p.Minute)
}

// formatAgo is "just now", "5 min ago", "3 h ago" or "2 days ago"; "never" for nothing.
func formatAgo(v js.Value, nowMs float64) string {
	if !v.Truthy() {
		return "never"
	}
	t := js.DateOf(v)
	if !js.IsValid(t) {
		return v.String()
	}
	seconds := js.Round((nowMs - t) / 1000)
	if seconds < 60 {
		return "just now"
	}
	minutes := js.Round(seconds / 60)
	if minutes < 60 {
		return js.NumberToString(minutes) + " min ago"
	}
	hours := js.Round(minutes / 60)
	if hours < 48 {
		return js.NumberToString(hours) + " h ago"
	}
	return js.NumberToString(js.Round(hours/24)) + " days ago"
}

// parseSeconds reads --timeout and --interval: a positive number of seconds, with an optional trailing "s".
func parseSeconds(input string) (float64, bool) {
	value := js.Trim(input)
	if strings.HasSuffix(value, "s") || strings.HasSuffix(value, "S") {
		value = value[:len(value)-1]
	}
	whole, frac, hasDot := strings.Cut(value, ".")
	if whole == "" || strings.Trim(whole, "0123456789") != "" || (hasDot && (frac == "" || strings.Trim(frac, "0123456789") != "")) {
		return 0, false
	}
	n := js.ToNumber(value)
	if !(n > 0) {
		return 0, false
	}
	return n, true
}
