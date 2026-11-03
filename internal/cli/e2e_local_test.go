package cli

import (
	"io"
	"net"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

// End to end against a LOCAL SMSTunnel API (never smstunnel.io) with a throwaway key and a fake phone, like the
// Node version's test/e2e/dev-api.e2e.test.mjs. Skipped unless one of these is set:
//
//	SMSTUNNEL_E2E_KEY_FILE   a JSON file {"serverUrl", "apiKey" or "keys": {...}} (SMSTUNNEL_E2E_KEY_NAME picks a key)
//	SMSTUNNEL_E2E_API_BASE and SMSTUNNEL_E2E_API_KEY
//
// SMSTUNNEL_E2E_FORWARD=host:port forwards a local port there (inside a container: host.docker.internal:3141),
// since the CLI takes http:// only for this computer. SMSTUNNEL_E2E_NUMBER is the recipient (+14155550123).
// It sends 3 texts: one sent and checked, one sign-in code, one sent twice with one Idempotency-Key.

type localAPI struct {
	base, key, number string
}

func localAPIFromEnv(t *testing.T) *localAPI {
	t.Helper()
	l := &localAPI{base: os.Getenv("SMSTUNNEL_E2E_API_BASE"), key: os.Getenv("SMSTUNNEL_E2E_API_KEY"), number: os.Getenv("SMSTUNNEL_E2E_NUMBER")}
	if l.number == "" {
		l.number = "+14155550123"
	}
	if file := os.Getenv("SMSTUNNEL_E2E_KEY_FILE"); file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("SMSTUNNEL_E2E_KEY_FILE: %v", err)
		}
		v, err := js.Parse(js.DecodeUTF8(data))
		if err != nil {
			t.Fatalf("SMSTUNNEL_E2E_KEY_FILE: %v", err)
		}
		if l.base == "" {
			l.base = v.Get("serverUrl").Str()
		}
		if name := os.Getenv("SMSTUNNEL_E2E_KEY_NAME"); name != "" {
			l.key = v.Get("keys").Get(name).Str()
		} else if l.key == "" {
			l.key = v.Get("apiKey").Str()
		}
	}
	if l.base == "" || l.key == "" {
		t.Skip("set SMSTUNNEL_E2E_KEY_FILE (or SMSTUNNEL_E2E_API_BASE and SMSTUNNEL_E2E_API_KEY) to run against a local API")
	}
	if !regexp.MustCompile(`^http://(127\.0\.0\.1|localhost)(:\d+)?(/|$)`).MatchString(l.base) {
		t.Fatalf("the local API must be http://127.0.0.1:PORT, never smstunnel.io: %s", l.base)
	}
	if target := os.Getenv("SMSTUNNEL_E2E_FORWARD"); target != "" {
		l.base = "http://" + forward(t, target)
	}
	normalized, err := normalizeAPIBase(l.base)
	if err != nil {
		t.Fatal(err)
	}
	l.base = normalized
	return l
}

// forward listens on 127.0.0.1 and copies each connection to target.
func forward(t *testing.T, target string) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				u, err := net.Dial("tcp", target)
				if err != nil {
					return
				}
				defer u.Close()
				go func() { _, _ = io.Copy(u, c) }()
				_, _ = io.Copy(c, u)
			}()
		}
	}()
	return ln.Addr().String()
}

func TestLocalAPIEndToEnd(t *testing.T) {
	l := localAPIFromEnv(t)
	cfg := newTempConfig(t)
	run := func(args []string, withKey bool, extra map[string]string, stdin string) cliResult {
		env := cfg.with(map[string]string{"SMSTUNNEL_POLL_INTERVAL_MS": "500"})
		if withKey {
			env["SMSTUNNEL_API_KEY"] = l.key
		}
		for k, v := range extra {
			env[k] = v
		}
		// --api-base first: a "--" among the arguments would make it part of the text.
		return runCLI(t, cliRun{args: append([]string{"--api-base", l.base}, args...), env: env, stdin: stdin})
	}

	login := run([]string{"login"}, false, nil, l.key+"\n")
	expectCode(t, login, 0)
	mustMatch(t, "login", login.stdout, `^Signed in with sk_live_`)
	mustMatch(t, "login", login.stdout, `Your phones: .*\(online\)`)
	if strings.Contains(login.stdout+login.stderr, l.key) {
		t.Fatal("the key was printed")
	}

	phones := run([]string{"phones"}, false, nil, "")
	expectCode(t, phones, 0)
	mustMatch(t, "phones", phones.stdout, `^ID\s+NAME\s+STATUS\s+LAST SEEN`)
	mustMatch(t, "phones", phones.stdout, `\bonline\b`)
	pj := run([]string{"phones", "--json"}, true, nil, "")
	if data, err := js.Parse(pj.stdout); err != nil || len(data.Get("devices").Items()) < 1 {
		t.Fatalf("phones --json: %s", pj.stdout)
	}

	sent := run([]string{"send", l.number, "smstunnel CLI (Go) e2e: hello from the command line", "--wait", "--timeout", "30", "--json"}, true, nil, "")
	expectCode(t, sent, 0)
	data, _ := js.Parse(sent.stdout)
	if !data.Get("result").StrictEquals("sent") || !data.Get("status").Get("deliveryStatus").StrictEquals("sent") {
		t.Fatalf("send --wait: %s", sent.stdout)
	}
	id := data.Get("messageId").Str()

	status := run([]string{"status", id}, true, nil, "")
	expectCode(t, status, 0)
	mustMatch(t, "status", status.stdout, `^Sent by your phone at `)
	mustMatch(t, "status", status.stdout, `To: \`+l.number)
	expectCode(t, run([]string{"status", "ffffffffffffffffffffffff"}, true, nil, ""), 5)

	code := run([]string{"code", l.number, "Your smstunnel CLI (Go) e2e sign-in code is 482913", "--wait", "--timeout", "30"}, true, nil, "")
	expectCode(t, code, 0)
	mustMatch(t, "code", code.stdout, `^Sent by your phone at `)

	key := "smstunnel-cli-go-e2e-" + time.Now().UTC().Format("20060102T150405.000000000")
	args := []string{"send", l.number, "smstunnel CLI (Go) e2e: idempotency " + key, "--idempotency-key", key, "--json"}
	first, again := run(args, true, nil, ""), run(args, true, nil, "")
	expectCode(t, first, 0)
	expectCode(t, again, 0)
	a, _ := js.Parse(first.stdout)
	b, _ := js.Parse(again.stdout)
	if a.Get("replayed").BoolVal() || !b.Get("replayed").BoolVal() || !b.Get("messageId").StrictEquals(a.Get("messageId").Str()) {
		t.Errorf("idempotency: %s / %s", first.stdout, again.stdout)
	}
	other := run([]string{"send", l.number, "smstunnel CLI (Go) e2e: another text, same key", "--idempotency-key", key}, true, nil, "")
	expectCode(t, other, 2)
	mustMatch(t, "reused", other.stderr, `already used in the last 24 hours for a different text`)

	received := run([]string{"received", "--since", "1d"}, true, nil, "")
	expectCode(t, received, 0)
	rj := run([]string{"received", "--json"}, true, nil, "")
	expectCode(t, rj, 0)
	if v, err := js.Parse(rj.stdout); err != nil || v.Get("messages").Kind() != js.Array || v.Get("hasMore").Kind() != js.Bool {
		t.Errorf("received --json: %s", rj.stdout)
	}

	usage := run([]string{"usage"}, true, nil, "")
	expectCode(t, usage, 0)
	mustMatch(t, "usage", usage.stdout, `^Plan: \w+`)
	mustMatch(t, "usage", usage.stdout, `Texts today: \d+ used`)

	refused := run([]string{"phones"}, false, map[string]string{"SMSTUNNEL_API_KEY": "sk_" + "live_" + strings.Repeat("0", 32)}, "")
	expectCode(t, refused, 3)
	mustMatch(t, "refused", refused.stderr, `refused the API key`)

	expectCode(t, run([]string{"logout"}, false, nil, ""), 0)
	expectCode(t, run([]string{"phones"}, false, nil, ""), 3)
}
