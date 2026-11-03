package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Test keys are built from pieces, so the repository holds no key-like literal.
var (
	keyFull     = "sk_" + "live_" + strings.Repeat("a", 32)
	keySendOnly = "sk_" + "live_" + strings.Repeat("b", 32)
	keyRefused  = "sk_" + "live_" + strings.Repeat("c", 32)
	keyHex      = "sk_" + "live_" + strings.Repeat("0123456789abcdef", 2)
)

// safeBuffer is a bytes.Buffer that a running CLI writes while the test reads it.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// fakeInput is standard input for tests: piped text, or a terminal that gets keys.
type fakeInput struct {
	r        io.Reader
	terminal bool
	keys     io.Reader
	rawCalls int
	restored int
}

func (f *fakeInput) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *fakeInput) IsTerminal() bool           { return f.terminal }
func (f *fakeInput) Raw() (io.Reader, func(), error) {
	f.rawCalls++
	return f.keys, func() { f.restored++ }, nil
}

// fakeTools records the key-store programs the CLI runs and answers like them.
type fakeTools struct {
	mu      sync.Mutex
	calls   []toolCall
	answer  func(call toolCall) (int, string, string)
	secrets map[string]string
}

type toolCall struct {
	name  string
	args  []string
	stdin string
}

func (f *fakeTools) run(name string, args []string, stdin string) (int, string, string) {
	f.mu.Lock()
	call := toolCall{name: name, args: append([]string(nil), args...), stdin: stdin}
	f.calls = append(f.calls, call)
	answer := f.answer
	f.mu.Unlock()
	if answer == nil {
		return -1, "", ""
	}
	return answer(call)
}

type cliRun struct {
	args       []string
	env        map[string]string
	stdin      string
	terminal   bool
	keys       string
	goos       string
	tools      toolRunner
	interrupts chan struct{}
	now        func() time.Time
}

type cliResult struct {
	code           int
	stdout, stderr string
}

func (r cliRun) envFor(t *testing.T, stdout, stderr io.Writer) *Env {
	goos := r.goos
	if goos == "" {
		goos = "linux"
	}
	vars := r.env
	tools := r.tools
	if tools == nil {
		tools = &fakeTools{}
	}
	now := r.now
	if now == nil {
		now = time.Now
	}
	env := &Env{
		Args:   r.args,
		Stdin:  &fakeInput{r: strings.NewReader(r.stdin), terminal: r.terminal, keys: strings.NewReader(r.keys)},
		Stdout: stdout,
		Stderr: stderr,
		Getenv: func(name string) string { return vars[name] },
		GOOS:   goos,
		Home:   func() string { return t.TempDir() },
		Now:    now,
		PID:    4242,
		Tools:  tools,
	}
	if r.interrupts != nil {
		ch := r.interrupts
		env.Interrupts = func() (<-chan struct{}, func()) { return ch, func() {} }
	} else {
		env.Interrupts = func() (<-chan struct{}, func()) { return make(chan struct{}), func() {} }
	}
	return env
}

func runCLI(t *testing.T, r cliRun) cliResult {
	t.Helper()
	var stdout, stderr safeBuffer
	code := Run(r.envFor(t, &stdout, &stderr))
	return cliResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// tempConfig is a fresh config folder and the variables that point the CLI at it (file store).
type tempConfig struct {
	dir  string
	file string
	env  map[string]string
}

func newTempConfig(t *testing.T) *tempConfig {
	dir := filepath.Join(t.TempDir(), "config")
	return &tempConfig{dir: dir, file: filepath.Join(dir, "config.json"),
		env: map[string]string{"SMSTUNNEL_CONFIG_DIR": dir, "SMSTUNNEL_KEY_STORE": "file"}}
}

func (c *tempConfig) with(extra map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range c.env {
		out[k] = v
	}
	for k, v := range extra {
		if v == "\x00unset" {
			delete(out, k)
		} else {
			out[k] = v
		}
	}
	return out
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func waitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
