package cli

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

// Scheduled texts: send --at and "smstunnel scheduled", which the Node.js version doesn't have.

var testZone = time.FixedZone("EET", 2*60*60)

// inTestZone makes this computer's time zone +02:00 (no daylight saving) for the rest of the test.
func inTestZone(t *testing.T) {
	saved := js.Location
	js.Location = testZone
	t.Cleanup(func() { js.Location = saved })
}

// atFromNow is a time d from now, to the minute: as typed for --at (local, +02:00), and as the CLI sends it.
func atFromNow(d time.Duration) (string, string) {
	at := time.Now().Add(d).Truncate(time.Minute)
	return at.In(testZone).Format("2006-01-02 15:04"), js.ISOString(float64(at.UnixMilli()))
}

func TestParseAt(t *testing.T) {
	inTestZone(t)
	for in, want := range map[string]string{
		"2026-11-16 09:00":               "2026-11-16T07:00:00.000Z",
		"2026-11-16T09:00":               "2026-11-16T07:00:00.000Z",
		"2026-11-16t09:00:30":            "2026-11-16T07:00:30.000Z",
		"  2026-11-16 09:00 ":            "2026-11-16T07:00:00.000Z",
		"2026-11-16T09:00:00Z":           "2026-11-16T09:00:00.000Z",
		"2026-11-16T09:00:00z":           "2026-11-16T09:00:00.000Z",
		"2026-11-16T09:00:00+02:00":      "2026-11-16T07:00:00.000Z",
		"2026-11-16 09:00-05:00":         "2026-11-16T14:00:00.000Z",
		"2026-11-16T09:00:00.5+00:00":    "2026-11-16T09:00:00.500Z",
		"2026-11-16T09:00:00.123456789Z": "2026-11-16T09:00:00.123Z",
		"2028-02-29 12:00":               "2028-02-29T10:00:00.000Z",
		"2026-12-31 23:59:59":            "2026-12-31T21:59:59.000Z",
	} {
		got, ok := parseAt(in)
		if !ok || js.ISOString(got) != want {
			t.Errorf("%q: %v %s, want %s", in, ok, js.ISOString(got), want)
		}
	}
	for _, in := range []string{
		"", "2026-11-16", "09:00", "tomorrow 09:00", "2026-11-16 9:00", "2026-11-16  09:00", "2026-11-16 24:00",
		"2026-11-16 09:60", "2026-11-16 09:00:60", "2026-02-29 09:00", "2026-13-01 09:00", "2026-11-31 09:00",
		"2026-11-16T09:00:00+2:00", "2026-11-16T09:00:00+0200", "2026-11-16T09:00:00+24:00", "2026-11-16T09:00:00.Z",
		"2026-11-16T09:00:00.1234567890Z", "2026-11-16 09:00 tomorrow", "2026-11-16 09", "2026-11-16 09:00:00:00",
		"2026-11-16T09:00Zulu", "2026-11-16/09:00",
	} {
		if got, ok := parseAt(in); ok {
			t.Errorf("%q was taken: %s", in, js.ISOString(got))
		}
	}
}

func TestFormatWhen(t *testing.T) {
	inTestZone(t)
	for in, want := range map[string]string{
		"2026-11-16T07:00:00Z": "2026-11-16 09:00 (2026-11-16 07:00 UTC)",
		"2026-11-16T07:00:30Z": "2026-11-16 09:00:30 (2026-11-16 07:00:30 UTC)",
		"2026-12-31T23:30:00Z": "2027-01-01 01:30 (2026-12-31 23:30 UTC)",
	} {
		if got := formatWhen(js.ParseDate(in)); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
	js.Location = time.UTC
	if got := formatWhen(js.ParseDate("2026-11-16T07:00:00Z")); got != "2026-11-16 07:00 UTC" {
		t.Errorf("in UTC: %q", got)
	}
}

func TestScheduledErrorMessages(t *testing.T) {
	ctx := func(op operation, at string, key bool) errorContext {
		return errorContext{operation: op, keySource: keyFromEnv, apiBase: "https://smstunnel.io/api/v1", idempotencyKey: key, scheduleAt: at}
	}
	refused := func(status int, code string, extra ...string) *apiError {
		e := &apiError{kind: "http", message: "Words from SMSTunnel.", status: js.IntValue(status), code: js.StringValue(code)}
		if len(extra) > 0 {
			e.permission = js.StringValue(extra[0])
		}
		return e
	}
	at := "2026-11-16 09:00 (2026-11-16 07:00 UTC)"
	full := refused(403, "SCHEDULE_LIMIT_REACHED")
	full.max = js.IntValue(50)
	dropped := &apiError{kind: "network", message: "socket hang up", networkCode: "ECONNRESET", mayHaveArrived: true}
	for _, c := range []struct {
		e    *apiError
		ctx  errorContext
		want string
		exit int
	}{
		{refused(400, "SCHEDULE_TOO_SOON"), ctx(opSend, at, false), "--at " + at + " is too soon: SMSTunnel schedules texts from 1 minute to 30 days ahead. To send the text now, leave out --at.", 2},
		{refused(400, "SCHEDULE_TOO_FAR"), ctx(opSend, at, false), "--at " + at + " is too far ahead: SMSTunnel schedules texts from 1 minute to 30 days ahead.", 2},
		{refused(400, "SCHEDULE_TOO_SOON"), ctx(opSend, "", false), "SMSTunnel refused the request: Words from SMSTunnel.", 2},
		{refused(400, "SCHEDULE_TEXT_TOO_LONG"), ctx(opSend, at, false), "SMSTunnel refused the request: Words from SMSTunnel.", 2},
		{full, ctx(opSend, at, false), `Your account already has the 50 scheduled texts your plan lets wait at once. Wait until some go out, or cancel one (see "smstunnel scheduled").`, 4},
		{refused(403, "SCHEDULE_LIMIT_REACHED"), ctx(opSend, at, false), `Your account already has as many scheduled texts as your plan lets wait at once. Wait until some go out, or cancel one (see "smstunnel scheduled").`, 4},
		{refused(404, "SCHEDULE_PHONE_NOT_FOUND"), ctx(opSend, at, false), `That phone isn't in your account. List your phones and their ids with "smstunnel phones".`, 5},
		{refused(403, "PERMISSION_REQUIRED", "sms:read"), ctx(opScheduled, "", false), `This API key may not list scheduled texts (it lacks the "sms:read" permission). ` + dashboardKeyHint + ".", 4},
		{refused(403, "PERMISSION_REQUIRED", "sms:send"), ctx(opCancel, "", false), `This API key may not cancel scheduled texts (it lacks the "sms:send" permission). ` + dashboardKeyHint + ".", 4},
		{refused(403, "PERMISSION_REQUIRED", "sms:send"), ctx(opSend, at, false), `This API key may not send texts (it lacks the "sms:send" permission). ` + dashboardKeyHint + ".", 4},
		{refused(404, "SCHEDULE_NOT_FOUND"), ctx(opCancel, "", false), `No scheduled text with that id for this key. "smstunnel scheduled" lists the ones waiting.`, 5},
		{refused(409, "SCHEDULE_NOT_EDITABLE"), ctx(opCancel, "", false), "This scheduled text can't be canceled any more: it already went out, failed or was canceled.", 2},
		{dropped, ctx(opSend, at, false), `The connection to SMSTunnel broke after the text was handed over, so it may have been scheduled. Check "smstunnel scheduled" before you schedule it again.`, 7},
		{dropped, ctx(opSend, at, true), "The connection to SMSTunnel broke after the text was handed over, so it may have been scheduled. Run the same command again with the same --idempotency-key: SMSTunnel will not schedule the text twice.", 7},
		{dropped, ctx(opCancel, "", false), "The connection to SMSTunnel broke after the request was sent, so the text may already be canceled. Run the same command again to find out.", 7},
		{refused(500, ""), ctx(opSend, at, false), `SMSTunnel had a problem (HTTP 500). The text may or may not have been scheduled: check "smstunnel scheduled" before you schedule it again.`, 7},
		{refused(500, ""), ctx(opSend, at, true), `SMSTunnel had a problem (HTTP 500). The text may or may not have been scheduled: check "smstunnel scheduled". Running the command again with the same key gets this answer again; use a new key only once you know the text was not scheduled.`, 7},
		{refused(429, ""), ctx(opScheduled, "", false), "Words from SMSTunnel.", 6},
	} {
		got := describeAPIError(c.e, c.ctx)
		if got.Message != c.want || got.ExitCode != c.exit {
			t.Errorf("%s %s in %s (at %q): got %q (%d)\nwant %q (%d)", c.e.status, c.e.code, c.ctx.operation, c.ctx.scheduleAt, got.Message, got.ExitCode, c.want, c.exit)
		}
	}
}

func TestMockSendAt(t *testing.T) {
	inTestZone(t)
	s := newMockSuite(t)
	local, wire := atFromNow(2 * time.Hour)
	r := s.run("send", "+40712345678", "Your appointment is today at 10:30", "--at", local)
	expectCode(t, r, 0)
	req := s.mock.lastRequest("/api/v1/sms/send", "POST")
	if got := js.Stringify(req.body, ""); got != `{"to":"+40712345678","message":"Your appointment is today at 10:30","sendAt":"`+wire+`"}` {
		t.Errorf("body: %s", got)
	}
	id := regexp.MustCompile(`Scheduled id: ([0-9a-f]{24})\.`).FindStringSubmatch(r.stdout)
	if id == nil {
		t.Fatalf("no scheduled id: %q", r.stdout)
	}
	want := "Scheduled for " + formatWhen(js.ParseDate(wire)) + ": SMSTunnel hands it to your phone then, which sends it from your number.\n" +
		"Scheduled id: " + id[1] + ". Cancel it with: smstunnel scheduled cancel " + id[1] + "\n"
	if r.stdout != want || r.stderr != "" {
		t.Errorf("stdout %q\nwant   %q\nstderr %q", r.stdout, want, r.stderr)
	}
	if !strings.Contains(r.stdout, local+" (") || !strings.Contains(r.stdout, " UTC): ") {
		t.Errorf("the local and UTC times: %q", r.stdout)
	}

	// The other options go as for any text; an RFC 3339 time is sent as the same moment in UTC.
	at := time.Now().Add(3 * time.Hour).Truncate(time.Minute)
	rfc, wire2 := at.In(time.FixedZone("", -5*60*60)).Format(time.RFC3339), js.ISOString(float64(at.UnixMilli()))
	expectCode(t, s.run("send", "0712345678", "Hi", "--phone", "Spare phone", "--country", "+40", "--at", rfc), 0)
	if got := js.Stringify(s.mock.lastRequest("/api/v1/sms/send", "POST").body, ""); got != `{"to":"0712345678","message":"Hi","deviceId":"b2b2b2b2b2b2b2b2b2b2b2b2","defaultCountryCode":"+40","sendAt":"`+wire2+`"}` {
		t.Errorf("body: %s", got)
	}

	j := s.run("send", "+40712345678", "JSON please", "--at", local, "--json")
	expectCode(t, j, 0)
	data, err := js.Parse(j.stdout)
	if err != nil || strings.Join(data.Keys(), ",") != "scheduledId,sendAt,replayed,parts,result,scheduled" {
		t.Fatalf("json: %s", j.stdout)
	}
	if !data.Get("result").StrictEquals("scheduled") || !data.Get("sendAt").StrictEquals(wire) || data.Get("replayed").BoolVal() ||
		!data.Get("scheduled").Get("status").StrictEquals("scheduled") || !data.Get("scheduled").Get("messageId").IsNull() {
		t.Errorf("json: %s", j.stdout)
	}
	if s.mock.textCount() != 0 || s.mock.scheduledCount() != 3 {
		t.Errorf("%d texts sent, %d scheduled; want 0 and 3", s.mock.textCount(), s.mock.scheduledCount())
	}
}

func TestMockSendAtWithAnIdempotencyKey(t *testing.T) {
	inTestZone(t)
	s := newMockSuite(t)
	local, _ := atFromNow(2 * time.Hour)
	args := []string{"send", "+40712345678", "Reminder", "--at", local, "--idempotency-key", "reminder-981"}
	first := s.run(args...)
	expectCode(t, first, 0)
	if got := s.mock.lastRequest("/api/v1/sms/send", "POST").header.Get("Idempotency-Key"); got != "reminder-981" {
		t.Errorf("header: %q", got)
	}
	again := s.run(args...)
	expectCode(t, again, 0)
	if !strings.HasPrefix(again.stdout, alreadyScheduled+"\nScheduled for ") || strings.Contains(first.stdout, "Already") {
		t.Errorf("first %q\nagain %q", first.stdout, again.stdout)
	}
	if s.mock.scheduledCount() != 1 {
		t.Errorf("%d scheduled, want 1", s.mock.scheduledCount())
	}
	j := s.run(append(args, "--json")...)
	if data, _ := js.Parse(j.stdout); !data.Get("replayed").BoolVal() {
		t.Errorf("json: %s", j.stdout)
	}
}

func TestMockSendAtRefusedBeforeCalling(t *testing.T) {
	inTestZone(t)
	s := newMockSuite(t)
	local, _ := atFromNow(2 * time.Hour)
	for _, c := range []struct {
		args    []string
		pattern string
	}{
		{[]string{"send", "+40712345678", "Hi", "--at", "tomorrow"}, `^--at takes a date and a time, like "2026-11-16 09:00" \(this computer's time zone\) or 2026-11-16T07:00:00Z\.\n$`},
		{[]string{"send", "+40712345678", "Hi", "--at", "2026-11-16"}, `^--at takes a date and a time`},
		{[]string{"send", "+40712345678", "Hi", "--at", local, "--wait"}, `^--wait doesn't go with --at: a scheduled text goes out at its time\. Follow it with "smstunnel scheduled"\.\n$`},
		{[]string{"code", "+40712345678", "Your code is 482913", "--at", local}, `^--at is not an option of "smstunnel code"\. See "smstunnel code --help"\.\n$`},
		{[]string{"phones", "--at", local}, `^--at is not an option of "smstunnel phones"`},
		{[]string{"send", "+40712345678", "Hi", "--at"}, `^--at needs a value\.\n$`},
	} {
		r := s.cli(c.args, keyFull, nil, "")
		if r.code != 2 {
			t.Errorf("%v: exit %d (%s)", c.args, r.code, r.stderr)
		}
		mustMatch(t, strings.Join(c.args, " "), r.stderr, c.pattern)
	}
	if n := s.mock.sendsSoFar(); n != 0 {
		t.Errorf("%d sends", n)
	}
}

func TestMockSendAtRefusals(t *testing.T) {
	inTestZone(t)
	s := newMockSuite(t)
	soon, _ := atFromNow(30 * time.Second)
	far, _ := atFromNow(40 * 24 * time.Hour)
	later, _ := atFromNow(2 * time.Hour)
	for _, c := range []struct {
		args    []string
		exit    int
		pattern string
	}{
		{[]string{"send", "+40712345678", "Hi", "--at", soon}, 2, `^--at .+ is too soon: SMSTunnel schedules texts from 1 minute to 30 days ahead\. To send the text now, leave out --at\.\n$`},
		{[]string{"send", "+40712345678", "Hi", "--at", far}, 2, `^--at .+ is too far ahead: SMSTunnel schedules texts from 1 minute to 30 days ahead\.\n$`},
		{[]string{"send", numOldServer, "Hi", "--at", later}, 2, `^SMSTunnel refused the request: property sendAt should not exist\n$`},
		{[]string{"send", numScheduleFull, "Hi", "--at", later}, 4, `^Your account already has the 10 scheduled texts your plan lets wait at once\. Wait until some go out, or cancel one \(see "smstunnel scheduled"\)\.\n$`},
		{[]string{"send", "+40712345678", "Hi", "--at", later, "--phone", "ffffffffffffffffffffffff"}, 5, `^That phone isn't in your account\.`},
		{[]string{"send", numServerProblem, "Hi", "--at", later}, 7, `The text may or may not have been scheduled: check "smstunnel scheduled" before you schedule it again\.\n$`},
		{[]string{"send", numConnectionDrops, "Hi", "--at", later}, 7, `so it may have been scheduled\. Check "smstunnel scheduled" before you schedule it again\.\n$`},
		{[]string{"send", numRateLimited, "Hi", "--at", later}, 6, `^Too many texts in a minute: SMSTunnel takes up to 30 send requests a minute\. Try again in 7 seconds\.\n$`},
	} {
		r := s.cli(c.args, keyFull, nil, "")
		if r.code != c.exit {
			t.Errorf("%v: exit %d, want %d (%s)", c.args, r.code, c.exit, r.stderr)
		}
		mustMatch(t, strings.Join(c.args, " "), r.stderr, c.pattern)
	}
	j := s.run("send", "+40712345678", "Hi", "--at", soon, "--json")
	expectCode(t, j, 2)
	e, _ := js.Parse(j.stderr)
	if !e.Get("error").Get("code").StrictEquals("SCHEDULE_TOO_SOON") || e.Get("error").Get("status").Num() != 400 || e.Get("error").Get("exitCode").Num() != 2 {
		t.Errorf("json error: %s", j.stderr)
	}
	if s.mock.scheduledCount() != 0 {
		t.Errorf("%d scheduled", s.mock.scheduledCount())
	}
}

// schedule schedules a text through the CLI and returns its id.
func (s *mockSuite) schedule(t *testing.T, to, message string, d time.Duration) string {
	t.Helper()
	local, _ := atFromNow(d)
	r := s.run("send", to, message, "--at", local)
	expectCode(t, r, 0)
	return regexp.MustCompile(`Scheduled id: ([0-9a-f]{24})\.`).FindStringSubmatch(r.stdout)[1]
}

func TestMockScheduledList(t *testing.T) {
	inTestZone(t)
	s := newMockSuite(t)
	empty := s.run("scheduled")
	expectCode(t, empty, 0)
	if empty.stdout != "No texts are waiting. Schedule one with: smstunnel send <number> <message…> --at \"YYYY-MM-DD HH:MM\"\n" {
		t.Errorf("empty: %q", empty.stdout)
	}
	later := s.schedule(t, "+40712345678", "See you\nat 10", 3*time.Hour)
	sooner := s.schedule(t, "+40722333444", "Your table is ready", 2*time.Hour)

	r := s.run("scheduled")
	expectCode(t, r, 0)
	lines := strings.Split(r.stdout, "\n")
	if len(lines) != 6 || lines[3] != "" || lines[5] != "" {
		t.Fatalf("lines: %q", r.stdout)
	}
	mustMatch(t, "header", lines[0], `^ID {24}SEND AT {11}TO {12}TEXT$`)
	mustMatch(t, "first", lines[1], `^`+sooner+`  \d{4}-\d{2}-\d{2} \d{2}:\d{2}  \+40722333444  Your table is ready$`)
	mustMatch(t, "second", lines[2], `^`+later+`  \d{4}-\d{2}-\d{2} \d{2}:\d{2}  \+40712345678  See you / at 10$`)
	if lines[4] != "2 waiting. Your account has 2 of the 10 texts your plan lets wait at once. Cancel one with: smstunnel scheduled cancel <id>" {
		t.Errorf("summary: %q", lines[4])
	}
	if q := s.mock.lastRequest("/api/v1/sms/scheduled", "GET"); q.rawURL != "/api/v1/sms/scheduled" {
		t.Errorf("request: %s", q.rawURL)
	}

	j := s.run("scheduled", "--json")
	expectCode(t, j, 0)
	data, _ := js.Parse(j.stdout)
	if items := data.Get("items").Items(); len(items) != 2 || !items[0].Get("id").StrictEquals(sooner) || data.Get("max").Num() != 10 || data.Get("waiting").Num() != 2 {
		t.Errorf("json: %s", j.stdout)
	}

	one := s.run("scheduled", "--limit", "1")
	expectCode(t, one, 0)
	if strings.Count(one.stdout, "\n") != 4 || one.stderr != "Showing 1 of 2. See the next ones with --page 2 (and --limit, up to 100).\n" {
		t.Errorf("one page: %q %q", one.stdout, one.stderr)
	}
	two := s.run("scheduled", "--limit", "1", "--page", "2")
	expectCode(t, two, 0)
	if q := s.mock.lastRequest("/api/v1/sms/scheduled", "GET"); q.rawURL != "/api/v1/sms/scheduled?limit=1&page=2" {
		t.Errorf("request: %s", q.rawURL)
	}
	if !strings.Contains(two.stdout, later) || strings.Contains(two.stdout, sooner) || two.stderr != "" {
		t.Errorf("page 2: %q %q", two.stdout, two.stderr)
	}

	// Another key of the account sees none of them, but they count for the account.
	other := s.cli([]string{"scheduled", "--json"}, keySendOnly, nil, "")
	expectCode(t, other, 0)
	if v, _ := js.Parse(other.stdout); len(v.Get("items").Items()) != 0 || v.Get("waiting").Num() != 2 {
		t.Errorf("other key: %s", other.stdout)
	}

	for _, args := range [][]string{{"scheduled", "--limit", "0"}, {"scheduled", "--limit", "101"}, {"scheduled", "--limit", "x"},
		{"scheduled", "--page", "0"}, {"scheduled", "--page", "1001"}, {"scheduled", "list"}, {"scheduled", "--since", "1h"}} {
		expectCode(t, s.run(args...), 2)
	}
}

func TestMockScheduledDone(t *testing.T) {
	inTestZone(t)
	s := newMockSuite(t)
	none := s.run("scheduled", "--done")
	expectCode(t, none, 0)
	if none.stdout != "No scheduled texts went out, failed or were canceled in the last 30 days.\n" {
		t.Errorf("none: %q", none.stdout)
	}
	if q := s.mock.lastRequest("/api/v1/sms/scheduled", "GET"); q.rawURL != "/api/v1/sms/scheduled?scope=done" {
		t.Errorf("request: %s", q.rawURL)
	}
	sent := s.schedule(t, "+40712345678", "Sent one", 2*time.Hour)
	failed := s.schedule(t, "+40712345678", "Failed one", 3*time.Hour)
	waiting := s.schedule(t, "+40712345678", "Still waiting", 4*time.Hour)
	s.mock.endScheduled(sent, "sent", "6ae9952f7f9ac207c547af90", "")
	s.mock.endScheduled(failed, "failed", "", "limit_reached")

	r := s.run("scheduled", "--done", "--limit", "10")
	expectCode(t, r, 0)
	if q := s.mock.lastRequest("/api/v1/sms/scheduled", "GET"); q.rawURL != "/api/v1/sms/scheduled?scope=done&limit=10" {
		t.Errorf("request: %s", q.rawURL)
	}
	lines := strings.Split(r.stdout, "\n")
	if len(lines) != 6 {
		t.Fatalf("lines: %q", r.stdout)
	}
	// Columns: SEND AT 16 wide, STATUS 22 ("failed (limit_reached)"), MESSAGE ID 24, TO 12; two spaces between.
	mustMatch(t, "header", lines[0], `^SEND AT {11}STATUS {18}MESSAGE ID {16}TO {12}TEXT$`)
	mustMatch(t, "failed", lines[1], `^\d{4}-\d{2}-\d{2} \d{2}:\d{2}  failed \(limit_reached\)  - {25}\+40712345678  Failed one$`)
	mustMatch(t, "sent", lines[2], `^\d{4}-\d{2}-\d{2} \d{2}:\d{2}  sent {20}6ae9952f7f9ac207c547af90  \+40712345678  Sent one$`)
	if lines[4] != "2 in the last 30 days. Check a sent one with: smstunnel status <message id>" {
		t.Errorf("summary: %q", lines[4])
	}
	if strings.Contains(r.stdout, waiting) || strings.Contains(r.stdout, "Still waiting") {
		t.Errorf("a waiting text in --done: %q", r.stdout)
	}
}

func TestMockScheduledCancel(t *testing.T) {
	inTestZone(t)
	s := newMockSuite(t)
	id := s.schedule(t, "+40712345678", "Cancel me", 2*time.Hour)
	r := s.run("scheduled", "cancel", id)
	expectCode(t, r, 0)
	if !regexp.MustCompile(`^Canceled: the text to \+40712345678 for \d{4}-\d{2}-\d{2} \d{2}:\d{2} \(\d{4}-\d{2}-\d{2} \d{2}:\d{2} UTC\) won't go out\.\n$`).MatchString(r.stdout) {
		t.Errorf("cancel: %q", r.stdout)
	}
	if q := s.mock.lastRequest("/api/v1/sms/scheduled/"+id, "DELETE"); q == nil || q.body.Kind() != js.Undefined {
		t.Errorf("DELETE request: %+v", q)
	}
	// Again: SMSTunnel answers 409, the CLI looks it up: already canceled is not an error.
	again := s.run("scheduled", "cancel", id)
	expectCode(t, again, 0)
	if !strings.HasPrefix(again.stdout, "Already canceled: the text to +40712345678 for ") || s.mock.lastRequest("/api/v1/sms/scheduled/"+id, "GET") == nil {
		t.Errorf("again: %q", again.stdout)
	}
	j := s.run("scheduled", "cancel", id, "--json")
	expectCode(t, j, 0)
	if v, _ := js.Parse(j.stdout); !v.Get("status").StrictEquals("canceled") || !v.Get("id").StrictEquals(id) {
		t.Errorf("json: %s", j.stdout)
	}

	sent := s.schedule(t, "+40712345678", "Gone", 3*time.Hour)
	s.mock.endScheduled(sent, "sent", "6ae9952f7f9ac207c547af90", "")
	late := s.run("scheduled", "cancel", sent)
	expectCode(t, late, 2)
	mustMatch(t, "sent", late.stderr, `^Too late to cancel: the text to \+40712345678 for .+ already went out\. Check it with: smstunnel status 6ae9952f7f9ac207c547af90\n$`)
	lj := s.run("scheduled", "cancel", sent, "--json")
	expectCode(t, lj, 2)
	mustMatch(t, "sent json", lj.stderr, `^\{"error":\{"message":"Too late to cancel: .*","exitCode":2,"status":409,"code":"SCHEDULE_NOT_EDITABLE","messageId":"6ae9952f7f9ac207c547af90"\}\}\n$`)

	failed := s.schedule(t, "+40712345678", "Failed", 4*time.Hour)
	s.mock.endScheduled(failed, "failed", "", "key_not_active")
	f := s.run("scheduled", "cancel", failed)
	expectCode(t, f, 2)
	mustMatch(t, "failed", f.stderr, `^Nothing to cancel: the text to \+40712345678 for .+ could not be sent at its time \(key_not_active\), so it did not go out\.\n$`)

	busy := s.schedule(t, "+40712345678", "Being sent", 5*time.Hour)
	s.mock.endScheduled(busy, "sending", "", "")
	b := s.run("scheduled", "cancel", busy)
	expectCode(t, b, 2)
	mustMatch(t, "sending", b.stderr, `^Too late to cancel: SMSTunnel is handing the text to \+40712345678 for .+ to your phone right now\.`)

	unknown := s.run("scheduled", "cancel", "5cffffffffffffffffffffff")
	expectCode(t, unknown, 5)
	if unknown.stderr != "No scheduled text with that id for this key. \"smstunnel scheduled\" lists the ones waiting.\n" {
		t.Errorf("unknown: %q", unknown.stderr)
	}
	mine := s.schedule(t, "+40712345678", "Not yours", 6*time.Hour)
	expectCode(t, s.cli([]string{"scheduled", "cancel", mine}, keySendOnly, nil, ""), 5)

	for _, c := range []struct {
		args    []string
		pattern string
	}{
		{[]string{"scheduled", "cancel"}, `^Give one scheduled text's id: smstunnel scheduled cancel <id>\n$`},
		{[]string{"scheduled", "cancel", id, id}, `^Give one scheduled text's id`},
		{[]string{"scheduled", "cancel", "abc"}, `^A scheduled text's id has 24 characters, 0-9 and a-f`},
		{[]string{"scheduled", "cancel", id, "--done"}, `^--done goes with the list: "smstunnel scheduled cancel" takes only the id\.\n$`},
		{[]string{"scheduled", "cancel", id, "--limit", "5"}, `^--limit goes with the list`},
	} {
		r := s.run(c.args...)
		if r.code != 2 {
			t.Errorf("%v: exit %d (%s)", c.args, r.code, r.stderr)
		}
		mustMatch(t, strings.Join(c.args, " "), r.stderr, c.pattern)
	}
}

func TestScheduledHelp(t *testing.T) {
	r := runCLI(t, cliRun{args: []string{"scheduled", "--help"}})
	expectCode(t, r, 0)
	if r.stdout != commandHelp["scheduled"]+"\n" {
		t.Errorf("help: %q", r.stdout)
	}
	mustMatch(t, "main help", mainHelp(), `(?m)^  scheduled \[cancel <id>\]     Texts you scheduled with send --at, or cancel one$`)
	mustMatch(t, "send help", commandHelp["send"], `(?m)^  --at <time>           Schedule it instead of sending it now`)
	if strings.Contains(commandHelp["code"], "--at") {
		t.Error("code help mentions --at")
	}
}
