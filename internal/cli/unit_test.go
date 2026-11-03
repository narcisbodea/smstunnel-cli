package cli

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

var testSpecs = []optionSpec{
	{name: "json", isBool: true},
	{name: "api-base"},
	{name: "help", isBool: true, short: 'h'},
	{name: "version", isBool: true, short: 'v'},
	{name: "phone"},
	{name: "wait", isBool: true},
}

func TestArgs(t *testing.T) {
	p, err := parseArgs([]string{"send", "--wait", "+40712345678", "Hello", "there", "--phone", "Store phone"}, testSpecs)
	if err != nil || !reflect.DeepEqual(p.positionals, []string{"send", "+40712345678", "Hello", "there"}) || !p.flag("wait") {
		t.Fatalf("%v %v", p, err)
	}
	if v, _ := p.str("phone"); v != "Store phone" || strings.Join(p.order, ",") != "wait,phone" {
		t.Errorf("options %v %v", p.options, p.order)
	}
	for _, args := range [][]string{{"--api-base=http://localhost:3101"}, {"--api-base", "http://localhost:3101"}} {
		p, _ := parseArgs(args, testSpecs)
		if v, _ := p.str("api-base"); v != "http://localhost:3101" {
			t.Errorf("%v: %q", args, v)
		}
	}
	p, _ = parseArgs([]string{"send", "+40712345678", "--", "--wait", "is", "part", "of", "the", "text"}, testSpecs)
	if !reflect.DeepEqual(p.positionals, []string{"send", "+40712345678", "--wait", "is", "part", "of", "the", "text"}) || len(p.options) != 0 {
		t.Errorf("after --: %v", p)
	}
	p, _ = parseArgs([]string{"send", "+40712345678", "-20%", "today", "-", "only", "-x", "-H"}, testSpecs)
	if !reflect.DeepEqual(p.positionals, []string{"send", "+40712345678", "-20%", "today", "-", "only", "-x", "-H"}) {
		t.Errorf("dashes: %v", p.positionals)
	}
	if p, _ := parseArgs([]string{"-h"}, testSpecs); !p.flag("help") {
		t.Error("-h")
	}
	if p, _ := parseArgs([]string{"-v"}, testSpecs); !p.flag("version") {
		t.Error("-v")
	}
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"phones", "--loud"}, `Unknown option --loud. See "smstunnel --help".`},
		{[]string{"send", "--phone"}, "--phone needs a value."},
		{[]string{"send", "--phone", "--wait"}, "--phone needs a value."},
		{[]string{"send", "--phone="}, "--phone needs a value."},
		{[]string{"send", "--wait=yes"}, "--wait takes no value."},
		{[]string{"--json=x"}, "--json takes no value."},
		{[]string{"--=x"}, `Unknown option --. See "smstunnel --help".`},
	} {
		_, err := parseArgs(c.args, testSpecs)
		if e, ok := err.(*CliError); !ok || e.ExitCode != 2 || e.Message != c.msg {
			t.Errorf("%v: %v", c.args, err)
		}
	}
	p, _ = parseArgs([]string{"--phone", "a", "--phone", "b"}, testSpecs)
	if v, _ := p.str("phone"); v != "b" {
		t.Errorf("last wins: %q", v)
	}
	p, _ = parseArgs([]string{"--phone", "-1"}, testSpecs)
	if v, _ := p.str("phone"); v != "-1" {
		t.Errorf("single dash value: %q", v)
	}
}

func TestDispatchOrder(t *testing.T) {
	for _, c := range []struct {
		args   []string
		code   int
		stdout string
		stderr string
	}{
		{[]string{"phones", "--wait", "--help"}, 0, commandHelp["phones"] + "\n", ""},
		{[]string{"blast", "--help"}, 2, "", `Unknown command "blast". See "smstunnel --help".` + "\n"},
		{[]string{"send", "x", "-v"}, 0, Version + "\n", ""},
		{[]string{"help", "send"}, 0, commandHelp["send"] + "\n", ""},
		{[]string{"help", "nope"}, 0, mainHelp() + "\n", ""},
		{[]string{"help"}, 0, mainHelp() + "\n", ""},
		{[]string{"--help"}, 0, mainHelp() + "\n", ""},
		{[]string{""}, 2, mainHelp() + "\n", ""},
		{[]string{"phones", "--since", "1h", "--wait"}, 2, "", `--since is not an option of "smstunnel phones". See "smstunnel phones --help".` + "\n"},
		{[]string{"phones", "--wait", "--since", "1h", "--wait"}, 2, "", `--wait is not an option of "smstunnel phones". See "smstunnel phones --help".` + "\n"},
		{[]string{"phones", "--api-base", "ftp://x"}, 2, "", "The API address must start with https:// (http:// only for localhost).\n"},
		{[]string{"phones", "--json", "--api-base", "ftp://x"}, 2, "", `{"error":{"message":"The API address must start with https:// (http:// only for localhost).","exitCode":2}}` + "\n"},
		{[]string{"phones", "--", "--json"}, 2, "", `{"error":{"message":"\"smstunnel phones\" takes no arguments.","exitCode":2}}` + "\n"},
		{[]string{"--json=x"}, 2, "", "--json takes no value.\n"},
	} {
		r := runCLI(t, cliRun{args: c.args, env: map[string]string{"SMSTUNNEL_CONFIG_DIR": t.TempDir()}})
		if r.code != c.code || r.stdout != c.stdout || r.stderr != c.stderr {
			t.Errorf("%q: exit %d\nstdout %q\nstderr %q", c.args, r.code, r.stdout, r.stderr)
		}
	}
}

func TestHelpTexts(t *testing.T) {
	h := mainHelp()
	if !strings.HasPrefix(h, "smstunnel "+Version+": send texts from your computer through your own Android phone and number.\n\nUsage: smstunnel <command> [options]") {
		t.Errorf("main help: %q", h[:80])
	}
	if !strings.HasSuffix(h, "130  stopped waiting with Ctrl+C (--wait); Ctrl+C ends received --watch with 0") {
		t.Error("main help ending")
	}
	for name, text := range commandHelp {
		if !strings.HasPrefix(text, "Usage: smstunnel "+name) {
			t.Errorf("%s help", name)
		}
	}
}

func TestFormatsFromNodeUnitTests(t *testing.T) {
	if got := maskKey(keyHex); got != "sk_"+"live_01…cdef" {
		t.Errorf("mask: %q", got)
	}
	if got := maskKey("sk_short"); got != "sk_…" {
		t.Errorf("short mask: %q", got)
	}
	status := func(s string) js.Value { v, _ := js.Parse(s); return v }
	for in, want := range map[string]string{
		`{"status":"pending","deliveryStatus":"pending"}`:                       "",
		`{"status":"sent","deliveryStatus":"sent"}`:                             "sent",
		`{"status":"delivered"}`:                                                "sent",
		`{"status":"failed","deliveryStatus":"failed"}`:                         "failed",
		`{"status":"failed","unconfirmed":true,"deliveryStatus":"unconfirmed"}`: "unconfirmed",
		`{"status":"failed","deliveryStatus":"unconfirmed"}`:                    "unconfirmed",
	} {
		if got := outcomeOf(status(in)); got != want {
			t.Errorf("outcome %s: %q", in, got)
		}
	}
	if got := describeStatus(status(`{"status":"failed","errorMessage":"No signal"}`)); got != "Not sent: No signal." {
		t.Errorf("failed: %q", got)
	}
	lines := phonesTable([]js.Value{
		status(`{"id":"aaaaaaaaaaaaaaaaaaaaaaaa","name":"Store phone","isOnline":true,"lastSeenAt":"2026-11-03T11:59:00Z"}`),
		status(`{"id":"bbbbbbbbbbbbbbbbbbbbbbbb","name":"Old Samsung","isOnline":false,"lastSeenAt":"2026-10-30T12:00:00Z"}`),
	}, js.ParseDate("2026-11-03T12:00:00Z"))
	want := []string{
		"ID                        NAME         STATUS   LAST SEEN",
		"aaaaaaaaaaaaaaaaaaaaaaaa  Store phone  online   now",
		"bbbbbbbbbbbbbbbbbbbbbbbb  Old Samsung  offline  4 days ago",
	}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("table:\n%s", strings.Join(lines, "\n"))
	}
}

func TestSinceSpansAtFixedNow(t *testing.T) {
	now := js.ParseDate("2026-11-03T12:00:00Z")
	for in, want := range map[string]string{
		"90s": "2026-11-03T11:58:30.000Z", "30m": "2026-11-03T11:30:00.000Z", "2h": "2026-11-03T10:00:00.000Z",
		"1d": "2026-11-02T12:00:00.000Z", "1w": "2026-10-27T12:00:00.000Z", " 2H ": "2026-11-03T10:00:00.000Z",
		"2026-11-03T08:00:00Z": "2026-11-03T08:00:00.000Z",
	} {
		got, ok := parseSince(in, now)
		if !ok || js.ISOString(got) != want {
			t.Errorf("%q: %v %v", in, ok, js.ISOString(got))
		}
	}
	for _, in := range []string{"yesterday", "2h ago", "-1h", "2026-13-45", "12", ""} {
		if _, ok := parseSince(in, now); ok {
			t.Errorf("%q was taken", in)
		}
	}
}

func TestRetryAfterDate(t *testing.T) {
	now := time.UnixMilli(1_000_000)
	got, ok := parseRetryAfter(time.UnixMilli(1_000_000+30_000).UTC().Format(time.RFC1123), now)
	if !ok || got != 30 {
		t.Errorf("date: %v %v", got, ok)
	}
	if _, ok := parseRetryAfter("soon", now); ok {
		t.Error("soon")
	}
}

func TestLibraryKeyCheck(t *testing.T) {
	for key, want := range map[string]bool{keyFull: true, "with space": false, " padded": false, "é": false, "": false} {
		if libraryTakesKey(key) != want {
			t.Errorf("%q", key)
		}
	}
	if !invalidHeaderValue("ș") || invalidHeaderValue("é ok\t") || !invalidHeaderValue("a\nb") {
		t.Error("header check")
	}
}
