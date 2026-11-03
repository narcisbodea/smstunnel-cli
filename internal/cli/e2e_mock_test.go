package cli

import (
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

// End to end against the mock API, the way the Node version's test/e2e/mock.e2e.test.mjs runs its CLI.

type mockSuite struct {
	t    *testing.T
	mock *mockAPI
	cfg  *tempConfig
}

func newMockSuite(t *testing.T) *mockSuite {
	return &mockSuite{t: t, mock: startMock(t), cfg: newTempConfig(t)}
}

// cli runs the CLI against the mock with the full key in SMSTUNNEL_API_KEY ("" key: none).
func (s *mockSuite) cli(args []string, key string, extra map[string]string, stdin string) cliResult {
	env := s.cfg.with(map[string]string{"SMSTUNNEL_POLL_INTERVAL_MS": "100"})
	if key != "" {
		env["SMSTUNNEL_API_KEY"] = key
	}
	for k, v := range extra {
		env[k] = v
	}
	return runCLI(s.t, cliRun{args: append(append([]string{}, args...), "--api-base", s.mock.apiBase), env: env, stdin: stdin})
}

func (s *mockSuite) run(args ...string) cliResult { return s.cli(args, keyFull, nil, "") }

func mustMatch(t *testing.T, what, text, pattern string) {
	t.Helper()
	if !regexp.MustCompile(pattern).MatchString(text) {
		t.Errorf("%s: %q does not match %s", what, text, pattern)
	}
}

func expectCode(t *testing.T, r cliResult, code int) {
	t.Helper()
	if r.code != code {
		t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", r.code, code, r.stdout, r.stderr)
	}
}

func TestMockVersionHelpUnknown(t *testing.T) {
	r := runCLI(t, cliRun{args: []string{"--version"}})
	expectCode(t, r, 0)
	if r.stdout != Version+"\n" {
		t.Errorf("version: %q", r.stdout)
	}
	r = runCLI(t, cliRun{args: []string{"--help"}})
	expectCode(t, r, 0)
	mustMatch(t, "help", r.stdout, `One number per text`)
	mustMatch(t, "help", r.stdout, `Exit codes:`)
	r = runCLI(t, cliRun{args: []string{"send", "--help"}})
	mustMatch(t, "send help", r.stdout, `--wait`)
	r = runCLI(t, cliRun{args: []string{"blast"}})
	expectCode(t, r, 2)
	mustMatch(t, "unknown", r.stderr, `Unknown command "blast"`)
	r = runCLI(t, cliRun{args: nil})
	expectCode(t, r, 2)
	// Object.prototype names are not commands here (the Node version trips over some of them).
	for _, name := range []string{"constructor", "toString", "__proto__", "hasOwnProperty"} {
		expectCode(t, runCLI(t, cliRun{args: []string{name}}), 2)
		if r := runCLI(t, cliRun{args: []string{"help", name}}); r.stdout != mainHelp()+"\n" {
			t.Errorf("help %s: %q", name, r.stdout)
		}
	}
}

func TestMockNoKey(t *testing.T) {
	s := newMockSuite(t)
	r := s.cli([]string{"phones"}, "", nil, "")
	expectCode(t, r, 3)
	mustMatch(t, "no key", r.stderr, `smstunnel login`)
	mustMatch(t, "no key", r.stderr, `dashboard\.smstunnel\.io → Apps → Add an app`)
}

func TestMockLoginRefusedKeyIsNeverSaved(t *testing.T) {
	s := newMockSuite(t)
	r := s.cli([]string{"login"}, "", nil, keyRefused+"\n")
	expectCode(t, r, 3)
	mustMatch(t, "login", r.stderr, `refused the API key`)
	if fileExists(s.cfg.file) {
		t.Error("config.json was written")
	}
}

func TestMockLoginKeyOnlyThroughStdin(t *testing.T) {
	s := newMockSuite(t)
	r := s.cli([]string{"login", keyFull}, "", nil, "")
	expectCode(t, r, 2)
	mustMatch(t, "login arg", r.stderr, `shell history`)
	expectCode(t, s.cli([]string{"login"}, "", nil, ""), 2)
}

func TestMockLoginThenSavedKeyThenLogout(t *testing.T) {
	s := newMockSuite(t)
	login := runCLI(t, cliRun{args: []string{"login", "--api-base", s.mock.apiBase}, env: s.cfg.env, stdin: keyFull + "\n"})
	expectCode(t, login, 0)
	mustMatch(t, "login", login.stdout, `Signed in with sk_live_aa…aaaa\. The key is saved in `)
	mustMatch(t, "login", login.stdout, `Your phones: Desk phone \(online\), Spare phone \(offline\)`)
	mustMatch(t, "login", login.stderr, `Warning: SMSTUNNEL_KEY_STORE=file, so the key is saved in a file`)
	if strings.Contains(login.stdout+login.stderr, keyFull) {
		t.Error("the key was printed")
	}
	saved, _ := js.Parse(readFile(t, s.cfg.file))
	want := js.NewObject().Set("keyStore", js.StringValue("file")).Set("apiBase", js.StringValue(s.mock.apiBase)).Set("apiKey", js.StringValue(keyFull))
	if js.Stringify(saved, "") != js.Stringify(want, "") {
		t.Errorf("config.json: %s", readFile(t, s.cfg.file))
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(s.cfg.file); info.Mode().Perm() != 0o600 {
			t.Errorf("config.json mode %v", info.Mode().Perm())
		}
		if info, _ := os.Stat(s.cfg.dir); info.Mode().Perm() != 0o700 {
			t.Errorf("config folder mode %v", info.Mode().Perm())
		}
	}
	phones := runCLI(t, cliRun{args: []string{"phones"}, env: s.cfg.env})
	expectCode(t, phones, 0)
	mustMatch(t, "phones", phones.stdout, `Desk phone`)
	env := runCLI(t, cliRun{args: []string{"phones"}, env: s.cfg.with(map[string]string{"SMSTUNNEL_API_KEY": keyRefused})})
	expectCode(t, env, 3)
	mustMatch(t, "env key", env.stderr, `SMSTUNNEL_API_KEY`)
	logout := runCLI(t, cliRun{args: []string{"logout"}, env: s.cfg.env})
	expectCode(t, logout, 0)
	mustMatch(t, "logout", logout.stdout, `Signed out: the key was removed from `)
	if fileExists(s.cfg.file) {
		t.Error("config.json is still there")
	}
	expectCode(t, runCLI(t, cliRun{args: []string{"phones"}, env: s.cfg.env}), 3)
}

func TestMockLoginHiddenInTerminal(t *testing.T) {
	s := newMockSuite(t)
	// Typed with a mistake fixed by Backspace, then Enter; the arrow key's escape sequence leaves "[A".
	keys := keyFull[:10] + "x\x7f" + keyFull[10:] + "\r"
	var stdout, stderr safeBuffer
	env := cliRun{args: []string{"login", "--api-base", s.mock.apiBase}, env: s.cfg.env, terminal: true, keys: keys}.envFor(t, &stdout, &stderr)
	code := Run(env)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Paste your SMSTunnel API key") || !strings.Contains(stderr.String(), "*\b \b") {
		t.Errorf("prompt: %q", stderr.String())
	}
	if strings.Count(stderr.String(), "*") != len(keyFull)+1 {
		t.Errorf("stars: %d", strings.Count(stderr.String(), "*"))
	}
	if in := env.Stdin.(*fakeInput); in.rawCalls != 1 || in.restored != 1 {
		t.Errorf("raw mode: %d on, %d restored", in.rawCalls, in.restored)
	}
	stop := cliRun{args: []string{"login", "--api-base", s.mock.apiBase}, env: s.cfg.env, terminal: true, keys: "sk_\x03"}
	r := runCLI(t, stop)
	expectCode(t, r, 130)
	mustMatch(t, "Ctrl+C", r.stderr, `Stopped\.\n$`)
}

func TestMockPhones(t *testing.T) {
	s := newMockSuite(t)
	r := s.run("phones")
	expectCode(t, r, 0)
	mustMatch(t, "table", r.stdout, `(?m)^ID\s+NAME\s+STATUS\s+LAST SEEN`)
	mustMatch(t, "table", r.stdout, `a1a1a1a1a1a1a1a1a1a1a1a1  Desk phone\s+online\s+now`)
	mustMatch(t, "table", r.stdout, `2 of 4 online`)
	j := s.run("phones", "--json")
	expectCode(t, j, 0)
	data, err := js.Parse(j.stdout)
	if err != nil || data.Get("total").Num() != 4 || !data.Get("devices").Items()[1].Get("name").StrictEquals("Spare phone") {
		t.Errorf("json: %s", j.stdout)
	}
	if !strings.HasPrefix(j.stdout, "{\n  \"devices\": [\n    {\n      \"id\": \"a1a1") {
		t.Errorf("not JSON.stringify(data, null, 2): %q", j.stdout[:40])
	}
}

func TestMockSendOneTextOneNumber(t *testing.T) {
	s := newMockSuite(t)
	r := s.run("send", "+40712345678", "Your", "order", "is", "ready")
	expectCode(t, r, 0)
	mustMatch(t, "send", r.stdout, `Handed to your phone, which sends it from your number\.`)
	mustMatch(t, "send", r.stdout, `Message id: [0-9a-f]{24}\. Check it with: smstunnel status [0-9a-f]{24}`)
	req := s.mock.lastRequest("/api/v1/sms/send", "POST")
	if got := js.Stringify(req.body, ""); got != `{"to":"+40712345678","message":"Your order is ready"}` {
		t.Errorf("body: %s", got)
	}
}

func TestMockSendPhoneAndCountry(t *testing.T) {
	s := newMockSuite(t)
	expectCode(t, s.run("send", "0712345678", "Hi", "--phone", "desk PHONE", "--country", "+40"), 0)
	if got := js.Stringify(s.mock.lastRequest("/api/v1/sms/send", "POST").body, ""); got != `{"to":"0712345678","message":"Hi","deviceId":"a1a1a1a1a1a1a1a1a1a1a1a1","defaultCountryCode":"+40"}` {
		t.Errorf("body: %s", got)
	}
	expectCode(t, s.run("send", "+40712345678", "Hi", "--phone", mockPhones[2].id), 0)
	if got := s.mock.lastRequest("/api/v1/sms/send", "POST").body.Get("deviceId"); !got.StrictEquals(mockPhones[2].id) {
		t.Errorf("deviceId: %s", got)
	}
	// The country code goes as typed (without spaces): the library would have written "+40".
	expectCode(t, s.run("send", "0712345678", "Hi", "--country", " 0 0 4 0 "), 0)
	if got := s.mock.lastRequest("/api/v1/sms/send", "POST").body.Get("defaultCountryCode"); !got.StrictEquals("0040") {
		t.Errorf("defaultCountryCode: %s", got)
	}
}

func TestMockSendRefusedBeforeCalling(t *testing.T) {
	s := newMockSuite(t)
	before := s.mock.sendsSoFar()
	for _, c := range []struct {
		args    []string
		pattern string
	}{
		{[]string{"send", "+40712345678,+40722333444", "Hi"}, `(?m)^One number per text\.$`},
		{[]string{"send", "+40712345678;+40722333444", "Hi"}, `(?m)^One number per text\.$`},
		{[]string{"send", "+40712345678 +40722333444", "Hi"}, `(?m)^One number per text\.$`},
		{[]string{"send", "0712345678 0722333444", "Hi"}, `more than one number\. One number per text\.`},
		{[]string{"send", "mom", "Hi"}, `not a phone number`},
		{[]string{"send", "+40712345678", "Hi", "--phone", "Twin"}, `Several of your phones are called "Twin"`},
		{[]string{"send", "+40712345678", "Hi", "--phone", "Kitchen"}, `No phone called "Kitchen"\. Your phones: "Desk phone", "Spare phone", "Twin", "Twin"`},
		{[]string{"send", "+40712345678", "Hi", "--country", "RO"}, `country calling code in digits`},
		{[]string{"send", "+40712345678", strings.Repeat("x", 1601)}, `The text has 1601 characters; a text can have up to 1600\.`},
		{[]string{"send", "+40712345678"}, `Write the message`},
		{[]string{"send", "+40712345678", "Hi", "--timeout", "5"}, `--timeout goes with --wait`},
		{[]string{"send"}, `Give the number to text`},
	} {
		r := s.cli(c.args, keyFull, nil, "")
		if r.code != 2 {
			t.Errorf("%v: exit %d (%s)", c.args, r.code, r.stderr)
		}
		mustMatch(t, strings.Join(c.args, " "), r.stderr, c.pattern)
	}
	if s.mock.sendsSoFar() != before {
		t.Error("something was sent")
	}
	r := s.run("phones", "--wait")
	expectCode(t, r, 2)
	mustMatch(t, "option", r.stderr, `--wait is not an option of "smstunnel phones"`)
}

func TestMockSendFromStdin(t *testing.T) {
	s := newMockSuite(t)
	expectCode(t, s.cli([]string{"send", "+40712345678"}, keyFull, nil, "Backup finished\n"), 0)
	if got := s.mock.lastRequest("/api/v1/sms/send", "POST").body.Get("message"); !got.StrictEquals("Backup finished") {
		t.Errorf("message: %s", got)
	}
	expectCode(t, s.cli([]string{"send", "+40712345678", "-"}, keyFull, nil, "Line one\r\nLine two\n\n"), 0)
	if got := s.mock.lastRequest("/api/v1/sms/send", "POST").body.Get("message"); !got.StrictEquals("Line one\nLine two") {
		t.Errorf("message: %s", got)
	}
	big := s.cli([]string{"send", "+40712345678", "-"}, keyFull, nil, strings.Repeat("x", 64*1024+1))
	expectCode(t, big, 2)
	mustMatch(t, "big", big.stderr, `The text on standard input is too long\.`)
}

func TestMockSendWait(t *testing.T) {
	s := newMockSuite(t)
	r := s.run("send", "+40712345678", "See you at 10", "--wait")
	expectCode(t, r, 0)
	mustMatch(t, "wait", r.stderr, `Handed to your phone \(message id [0-9a-f]{24}\)\. Waiting up to 120 s`)
	mustMatch(t, "wait", r.stdout, `(?m)^Sent by your phone at \d{4}-\d{2}-\d{2} \d{2}:\d{2}\. Message id: [0-9a-f]{24}$`)

	j := s.run("send", "+40712345678", "JSON please", "--wait", "--json")
	expectCode(t, j, 0)
	data, _ := js.Parse(j.stdout)
	if !data.Get("result").StrictEquals("sent") || data.Get("queued").BoolVal() || !data.Get("status").Get("deliveryStatus").StrictEquals("sent") {
		t.Errorf("json: %s", j.stdout)
	}
	keys := strings.Join(data.Keys(), ",")
	if keys != "messageId,queued,branded,replayed,parts,remaining,result,status" {
		t.Errorf("members: %s", keys)
	}

	failed := s.run("send", "+40712345678", "FAILME please", "--wait")
	expectCode(t, failed, 8)
	mustMatch(t, "failed", failed.stderr, `Not sent: Generic failure \(the SIM refused it\)\. Message id: [0-9a-f]{24}`)

	unconfirmed := s.run("send", "+40712345678", "UNCONFIRMED text", "--wait")
	expectCode(t, unconfirmed, 0)
	mustMatch(t, "unconfirmed", unconfirmed.stdout, `Not confirmed by your phone: .*most likely sent\. Don't send it again\.`)

	throttled := s.run("send", "+40712345678", "STATUS429ONCE text", "--wait")
	expectCode(t, throttled, 0)
	mustMatch(t, "429 once", throttled.stdout, `Sent by your phone`)

	forbidden := s.run("send", "+40712345678", "STATUS403 text", "--wait")
	expectCode(t, forbidden, 4)
	mustMatch(t, "403", forbidden.stderr, `may not check whether texts went out .* The text itself was handed over \(message id [0-9a-f]{24}\): don't send it again\.`)
	fj := s.run("send", "+40712345678", "STATUS403 json", "--wait", "--json")
	expectCode(t, fj, 4)
	mustMatch(t, "403 json", fj.stderr, `^\{"error":\{"message":".*","exitCode":4,"status":403,"code":"PERMISSION_REQUIRED","permission":"sms:read","messageId":"[0-9a-f]{24}"\}\}\n$`)
}

func TestMockOfflinePhone(t *testing.T) {
	s := newMockSuite(t)
	q := s.run("send", "+40712345678", "Hi", "--phone", "Spare phone")
	expectCode(t, q, 0)
	mustMatch(t, "queued", q.stdout, `(?m)^Queued: none of your phones is online right now\. The text goes out when your phone is back online \(within 3 days\)\.$`)
	j := s.run("send", "+40712345678", "Hi", "--phone", "Spare phone", "--json")
	if data, _ := js.Parse(j.stdout); !data.Get("result").StrictEquals("queued") {
		t.Errorf("json: %s", j.stdout)
	}
	w := s.run("send", "+40712345678", "Hi", "--phone", "Spare phone", "--wait", "--timeout", "1")
	expectCode(t, w, 9)
	mustMatch(t, "still waiting", w.stderr, `Still waiting for your phone after 1 s \(it is offline\)\. .*don't send it again\. Check it later with: smstunnel status [0-9a-f]{24}`)
}

func TestMockRefusals(t *testing.T) {
	s := newMockSuite(t)
	r := s.run("send", numRateLimited, "Hi")
	expectCode(t, r, 6)
	if r.stderr != "Too many texts in a minute: SMSTunnel takes up to 30 send requests a minute. Try again in 7 seconds.\n" {
		t.Errorf("429: %q", r.stderr)
	}
	r = s.run("send", numAllowanceUsed, "Hi")
	expectCode(t, r, 6)
	if r.stderr != "You've sent today's 25 texts on the Free plan. More go out tomorrow.\n" {
		t.Errorf("429 allowance: %q", r.stderr)
	}
	r = s.run("send", numPlanRefused, "Hi")
	expectCode(t, r, 4)
	mustMatch(t, "403", r.stderr, `Professional or Enterprise plan`)
	r = s.run("send", numValidation, "Hi")
	expectCode(t, r, 2)
	mustMatch(t, "400", r.stderr, `Phone number must contain 7-25 characters; second problem`)
	r = s.run("send", numServerProblem, "Hi")
	expectCode(t, r, 7)
	mustMatch(t, "500", r.stderr, `may or may not have been taken`)
	r = s.run("send", numConnectionDrops, "Hi")
	expectCode(t, r, 7)
	mustMatch(t, "dropped", r.stderr, `may have gone out\. Check the Messages page`)
	j := s.cli([]string{"phones", "--json"}, keyRefused, nil, "")
	expectCode(t, j, 3)
	if j.stdout != "" {
		t.Errorf("stdout: %q", j.stdout)
	}
	e, _ := js.Parse(j.stderr)
	if e.Get("error").Get("exitCode").Num() != 3 || e.Get("error").Get("status").Num() != 401 {
		t.Errorf("json error: %s", j.stderr)
	}
	mustMatch(t, "json error", e.Get("error").Get("message").Str(), `refused the API key`)
}

func TestMockCode(t *testing.T) {
	s := newMockSuite(t)
	before := s.mock.sendsSoFar()
	r := s.run("code", "+40712345678", "Hello there")
	expectCode(t, r, 2)
	mustMatch(t, "no code", r.stderr, `must contain the code`)
	r = s.run("code", "+40712345678", "Code 482913 "+strings.Repeat("x", 320))
	expectCode(t, r, 2)
	mustMatch(t, "long", r.stderr, `a sign-in code text can have up to 320`)
	if s.mock.sendsSoFar() != before {
		t.Error("something was sent")
	}
	r = s.run("code", "+40712345678", "Your", "sign-in", "code", "is", "482913", "--wait")
	expectCode(t, r, 0)
	mustMatch(t, "code", r.stdout, `Sent by your phone`)
	if got := js.Stringify(s.mock.lastRequest("/api/v1/sms/send-2fa", "POST").body, ""); got != `{"to":"+40712345678","message":"Your sign-in code is 482913"}` {
		t.Errorf("body: %s", got)
	}
}

func TestMockStatus(t *testing.T) {
	s := newMockSuite(t)
	sent := s.run("send", "+40712345678", "Status check", "--wait", "--json")
	data, _ := js.Parse(sent.stdout)
	id := data.Get("messageId").Str()
	r := s.run("status", id)
	expectCode(t, r, 0)
	mustMatch(t, "status", r.stdout, `^Sent by your phone at .*\.\nTo: \+40712345678\nMessage id: [0-9a-f]{24}\n$`)
	j := s.run("status", id, "--json")
	if v, _ := js.Parse(j.stdout); !v.Get("status").StrictEquals("sent") {
		t.Errorf("json: %s", j.stdout)
	}
	unknown := s.run("status", "ffffffffffffffffffffffff")
	expectCode(t, unknown, 5)
	if unknown.stderr != "No text with that id for this key.\n" {
		t.Errorf("unknown: %q", unknown.stderr)
	}
	expectCode(t, s.run("status", "abc"), 2)
}

func TestMockReceived(t *testing.T) {
	s := newMockSuite(t)
	r := s.run("received")
	expectCode(t, r, 0)
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines: %q", r.stdout)
	}
	mustMatch(t, "first", lines[0], `\+40711111111  \(Desk phone\)  Earlier reply$`)
	mustMatch(t, "second", lines[1], `\+40722222222  \(Desk phone\)  See you / at 10$`)
	since := s.run("received", "--since", "1h", "--from", "+40722222222", "--limit", "5")
	expectCode(t, since, 0)
	q := s.mock.lastRequest("/api/v1/sms/received", "GET")
	if q.query.Get("limit") != "5" || q.query.Get("phone") != "+40722222222" {
		t.Errorf("query: %v", q.query)
	}
	if !strings.HasPrefix(q.rawURL, "/api/v1/sms/received?limit=5&since=") || !strings.HasSuffix(q.rawURL, "&phone=%2B40722222222") {
		t.Errorf("not the Node version's query order and encoding: %s", q.rawURL)
	}
	if d := js.ParseDate(q.query.Get("since")) - float64(time.Now().Add(-time.Hour).UnixMilli()); d > 60000 || d < -60000 {
		t.Errorf("since: %s", q.query.Get("since"))
	}
	if n := len(strings.Split(strings.TrimSpace(since.stdout), "\n")); n != 1 {
		t.Errorf("lines: %q", since.stdout)
	}
	expectCode(t, s.run("received", "--since", "yesterday"), 2)
	expectCode(t, s.run("received", "--limit", "0"), 2)
	inbox := s.cli([]string{"received"}, keySendOnly, nil, "")
	expectCode(t, inbox, 4)
	mustMatch(t, "inbox", inbox.stderr, `may not read received texts`)
	mustMatch(t, "inbox", inbox.stderr, `choose "Allow reading received texts" for this key, or add an app with "Can also read received texts" ticked`)
	huge := s.run("received", "--since", "999999999999999d")
	expectCode(t, huge, 1)
	if huge.stderr != "Unexpected error: RangeError: Invalid time value\n" {
		t.Errorf("Node's RangeError: %q", huge.stderr)
	}
}

func TestMockReceivedWatch(t *testing.T) {
	s := newMockSuite(t)
	interrupts := make(chan struct{}, 1)
	var stdout, stderr safeBuffer
	env := cliRun{args: []string{"received", "--watch", "--interval", "3", "--api-base", s.mock.apiBase},
		env: s.cfg.with(map[string]string{"SMSTUNNEL_API_KEY": keyFull, "SMSTUNNEL_POLL_INTERVAL_MS": "100"}), interrupts: interrupts}.envFor(t, &stdout, &stderr)
	done := make(chan int, 1)
	go func() { done <- Run(env) }()
	waitFor(t, "the first texts", 10*time.Second, func() bool { return strings.Contains(stdout.String(), "See you / at 10") })
	s.mock.addReceived("abababababababababababab", "+40733333333", "New reply while watching")
	waitFor(t, "the new text", 10*time.Second, func() bool { return strings.Contains(stdout.String(), "New reply while watching") })
	time.Sleep(300 * time.Millisecond) // a few more polls: the text must not come twice
	interrupts <- struct{}{}
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Ctrl+C did not stop watching")
	}
	mustMatch(t, "watch", stdout.String(), `See you / at 10\n.*\+40733333333  \(Desk phone\)  New reply while watching`)
	if n := strings.Count(stdout.String(), "New reply while watching"); n != 1 {
		t.Errorf("shown %d times", n)
	}
	mustMatch(t, "watch", stderr.String(), `Watching for new texts every 3 s`)
}

func TestMockWaitInterrupted(t *testing.T) {
	s := newMockSuite(t)
	interrupts := make(chan struct{}, 1)
	var stdout, stderr safeBuffer
	env := cliRun{args: []string{"send", "+40712345678", "Hi", "--phone", "Spare phone", "--wait", "--api-base", s.mock.apiBase},
		env: s.cfg.with(map[string]string{"SMSTUNNEL_API_KEY": keyFull, "SMSTUNNEL_POLL_INTERVAL_MS": "100"}), interrupts: interrupts}.envFor(t, &stdout, &stderr)
	done := make(chan int, 1)
	go func() { done <- Run(env) }()
	waitFor(t, "the waiting note", 10*time.Second, func() bool { return strings.Contains(stderr.String(), "Waiting up to 120 s") })
	interrupts <- struct{}{}
	select {
	case code := <-done:
		if code != 130 {
			t.Errorf("exit %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Ctrl+C did not stop waiting")
	}
	mustMatch(t, "interrupted", stderr.String(), `Stopped waiting\. The text is still on its way; check it with: smstunnel status [0-9a-f]{24}\n$`)
}

func TestMockUsage(t *testing.T) {
	s := newMockSuite(t)
	r := s.run("usage")
	expectCode(t, r, 0)
	want := "Plan: free\nTexts today: 3 used, 22 left (of 25)\nTexts this month: 40 used, 460 left (of 500)\n" +
		"Texts with the free line today: 0 used, 25 left (of 25)\nSign-in codes today: 1 used, 19 left (of 20)\n"
	if r.stdout != want {
		t.Errorf("usage: %q", r.stdout)
	}
}

func TestMockAPIAddresses(t *testing.T) {
	s := newMockSuite(t)
	env := s.cfg.with(map[string]string{"SMSTUNNEL_API_KEY": keyFull})
	r := runCLI(t, cliRun{args: []string{"phones", "--api-base", "http://example.com/api/v1"}, env: env})
	expectCode(t, r, 2)
	mustMatch(t, "http", r.stderr, `https://`)
	r = runCLI(t, cliRun{args: []string{"phones", "--api-base", strings.Replace(s.mock.apiBase, "/api/v1", "/redirect-base/api/v1", 1)}, env: env})
	expectCode(t, r, 7)
	mustMatch(t, "redirect", r.stderr, `redirect`)
	r = runCLI(t, cliRun{args: []string{"phones", "--api-base", strings.Replace(s.mock.apiBase, "/api/v1", "/html-base/api/v1", 1)}, env: env})
	expectCode(t, r, 7)
	mustMatch(t, "html", r.stderr, `not the SMSTunnel API`)
	r = runCLI(t, cliRun{args: []string{"phones", "--api-base", "http://127.0.0.1:9/api/v1"}, env: env})
	expectCode(t, r, 7)
	mustMatch(t, "down", r.stderr, `Could not reach SMSTunnel at 127\.0\.0\.1:9`)
	// SMSTUNNEL_API_BASE, then the saved one.
	r = runCLI(t, cliRun{args: []string{"phones"}, env: s.cfg.with(map[string]string{"SMSTUNNEL_API_KEY": keyFull, "SMSTUNNEL_API_BASE": s.mock.server.URL})})
	expectCode(t, r, 0)
}

func TestMockIdempotencyKey(t *testing.T) {
	s := newMockSuite(t)
	key := "order-1042-ready"
	first := s.run("send", "+40712345678", "Order 1042 is ready", "--idempotency-key", key)
	expectCode(t, first, 0)
	if got := s.mock.lastRequest("/api/v1/sms/send", "POST").header.Get("Idempotency-Key"); got != key {
		t.Errorf("header: %q", got)
	}
	if strings.Contains(first.stdout, "Already handed over") {
		t.Error("replayed on the first request")
	}
	id := regexp.MustCompile(`Message id: ([0-9a-f]{24})`).FindStringSubmatch(first.stdout)[1]
	texts := s.mock.textCount()
	again := s.run("send", "+40712345678", "Order 1042 is ready", "--idempotency-key", key)
	expectCode(t, again, 0)
	want := alreadySent + "\nHanded to your phone, which sends it from your number.\nMessage id: " + id + ". Check it with: smstunnel status " + id + "\n"
	if again.stdout != want {
		t.Errorf("again: %q", again.stdout)
	}
	j := s.run("send", "+40712345678", "Order 1042 is ready", "--idempotency-key", key, "--json")
	if data, _ := js.Parse(j.stdout); !data.Get("replayed").BoolVal() || !data.Get("messageId").StrictEquals(id) || !data.Get("result").StrictEquals("handed-over") {
		t.Errorf("json: %s", j.stdout)
	}
	waited := s.run("send", "+40712345678", "Order 1042 is ready", "--idempotency-key", key, "--wait")
	expectCode(t, waited, 0)
	mustMatch(t, "wait", waited.stderr, `^Already handed over with this --idempotency-key`)
	mustMatch(t, "wait", waited.stdout, `(?m)^Sent by your phone at .*\. Message id: `+id+`$`)
	if s.mock.textCount() != texts {
		t.Error("a second text")
	}
	plain := s.run("send", "+40712345678", "Without a key", "--json")
	if data, _ := js.Parse(plain.stdout); data.Get("replayed").BoolVal() {
		t.Errorf("plain: %s", plain.stdout)
	}
	if got := s.mock.lastRequest("/api/v1/sms/send", "POST").header.Values("Idempotency-Key"); len(got) != 0 {
		t.Errorf("header without a key: %v", got)
	}

	reused := s.run("send", "+40712345678", "A different text", "--idempotency-key", key)
	expectCode(t, reused, 2)
	if reused.stderr != `This --idempotency-key was already used in the last 24 hours for a different text (another number, message, phone or country code, or "code" instead of "send"). Nothing was sent. Use a new key for a new text.`+"\n" {
		t.Errorf("reused: %q", reused.stderr)
	}
	rj := s.run("code", "+40712345678", "Order 1042 is ready 482913", "--idempotency-key", key, "--json")
	expectCode(t, rj, 2)
	if e, _ := js.Parse(rj.stderr); !e.Get("error").Get("code").StrictEquals("IDEMPOTENCY_KEY_REUSED") {
		t.Errorf("reused json: %s", rj.stderr)
	}

	busy := s.run("send", "+40712345678", "Hi", "--idempotency-key", idemInProgress)
	expectCode(t, busy, 6)
	if busy.stderr != "SMSTunnel is still handling an earlier request with this --idempotency-key. Run the same command again in 1 second to get its answer; it will not send a second text.\n" {
		t.Errorf("busy: %q", busy.stderr)
	}

	before := s.mock.sendsSoFar()
	for _, v := range []string{strings.Repeat("x", 256), "commande-é", "   "} {
		r := s.run("send", "+40712345678", "Hi", "--idempotency-key", v)
		expectCode(t, r, 2)
		mustMatch(t, "bad key", r.stderr, `^--idempotency-key takes 1 to 255 printable ASCII characters`)
	}
	if s.mock.sendsSoFar() != before {
		t.Error("sent with a bad key")
	}
	p := s.run("phones", "--idempotency-key", "k")
	expectCode(t, p, 2)
	mustMatch(t, "phones", p.stderr, `--idempotency-key is not an option of "smstunnel phones"`)

	c := s.run("code", "+40712345678", "Your sign-in code is 552211", "--idempotency-key", "login-552211")
	expectCode(t, c, 0)
	req := s.mock.lastRequest("/api/v1/sms/send-2fa", "POST")
	if req.header.Get("Idempotency-Key") != "login-552211" || js.Stringify(req.body, "") != `{"to":"+40712345678","message":"Your sign-in code is 552211"}` {
		t.Errorf("code request: %v %s", req.header, js.Stringify(req.body, ""))
	}

	p1 := s.run("send", numServerProblem, "Hi", "--idempotency-key", "server-problem-1")
	expectCode(t, p1, 7)
	mustMatch(t, "500 key", p1.stderr, `(?m)^SMSTunnel had a problem \(HTTP 500\)\. The text may or may not have been taken: .*Running the command again with the same key gets this answer again; use a new key only once you know the text did not go out\.$`)
	p2 := s.run("send", numServerProblem, "Hi", "--idempotency-key", "server-problem-1")
	expectCode(t, p2, 7)
	mustMatch(t, "500 replayed", p2.stderr, `^SMSTunnel had a problem \(HTTP 500\) with the first request that used this --idempotency-key, and gives that same answer for 24 hours\.`)
	d := s.run("send", numConnectionDrops, "Hi", "--idempotency-key", "dropped-1")
	expectCode(t, d, 7)
	if d.stderr != "The connection to SMSTunnel broke after the text was handed over, so it may have gone out. Run the same command again with the same --idempotency-key: SMSTunnel will not send the text twice.\n" {
		t.Errorf("dropped: %q", d.stderr)
	}
}

func TestMockEveryRequest(t *testing.T) {
	s := newMockSuite(t)
	s.run("phones")
	s.run("send", "+40712345678", "Hi", "--wait")
	s.run("received", "--phone", "Desk phone")
	s.run("usage")
	s.mock.mu.Lock()
	defer s.mock.mu.Unlock()
	if len(s.mock.requests) < 6 {
		t.Fatalf("%d requests", len(s.mock.requests))
	}
	ua := regexp.MustCompile(`^smstunnel-cli/\d+\.\d+\.\d+ \(go\d`)
	for _, r := range s.mock.requests {
		if strings.Contains(r.rawURL, "sk_") {
			t.Errorf("a key in %s", r.rawURL)
		}
		if r.path == "/api/v1/sms/send-bulk" {
			t.Error("send-bulk")
		}
		if !ua.MatchString(r.header.Get("User-Agent")) {
			t.Errorf("User-Agent %q", r.header.Get("User-Agent"))
		}
		if r.header.Get("Authorization") != "" || r.header.Get("Accept-Encoding") != "" {
			t.Errorf("headers %v", r.header)
		}
		if r.header.Get("Accept") != "application/json" || r.header.Get("X-API-Key") != keyFull {
			t.Errorf("headers %v", r.header)
		}
	}
}
