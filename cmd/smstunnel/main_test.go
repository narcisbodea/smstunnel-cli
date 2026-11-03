package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// These tests run the built binary, the way people run it: stdin closed, a fresh config folder, the file store.

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "smstunnel-bin-")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "smstunnel")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		panic(string(out))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

var testKey = "sk_" + "live_" + strings.Repeat("a", 32)

// stub answers like SMSTunnel for one online phone whose texts stay pending, and no received texts.
func stub(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Header.Get("X-API-Key") != testKey:
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"statusCode":401,"message":"Invalid or expired API key"}`)
		case r.URL.Path == "/api/v1/sms/send":
			_, _ = io.WriteString(w, `{"success":true,"messageId":"000000000000000000000001","data":{"messageId":"000000000000000000000001","branded":false}}`)
		case strings.HasPrefix(r.URL.Path, "/api/v1/sms/status/"):
			_, _ = io.WriteString(w, `{"success":true,"data":{"id":"000000000000000000000001","status":"pending","deliveryStatus":"pending"}}`)
		case r.URL.Path == "/api/v1/sms/received":
			_, _ = io.WriteString(w, `{"success":true,"data":{"messages":[],"total":0,"page":1,"limit":20,"hasMore":false}}`)
		default:
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"statusCode":404,"message":"Not found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/api/v1"
}

func command(t *testing.T, args ...string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	cmd := exec.Command(binary, args...)
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(kv), "SMSTUNNEL_") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "SMSTUNNEL_CONFIG_DIR="+t.TempDir(), "SMSTUNNEL_KEY_STORE=file", "SMSTUNNEL_POLL_INTERVAL_MS=100", "SMSTUNNEL_API_KEY="+testKey)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	return cmd, &stdout, &stderr
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if e, ok := err.(*exec.ExitError); ok {
		return e.ExitCode()
	}
	return -1
}

func TestBinaryBasics(t *testing.T) {
	cmd, stdout, _ := command(t, "--version")
	if err := cmd.Run(); err != nil || stdout.String() != "1.0.0\n" {
		t.Errorf("--version: %v %q", err, stdout.String())
	}
	cmd, stdout, _ = command(t, "--help")
	if err := cmd.Run(); err != nil || !strings.Contains(stdout.String(), "Exit codes:") {
		t.Errorf("--help: %v", err)
	}
	cmd, _, stderr := command(t, "phones")
	cmd.Env = append(cmd.Env, "SMSTUNNEL_API_KEY=")
	if code := exitCode(cmd.Run()); code != 3 || !strings.Contains(stderr.String(), "smstunnel login") {
		t.Errorf("no key: exit %d %q", code, stderr.String())
	}
	// stdin closed (not a terminal): "send <number>" reads the text from it, which is empty.
	cmd, _, stderr = command(t, "send", "+14155550123", "--api-base", stub(t))
	if code := exitCode(cmd.Run()); code != 2 || !strings.Contains(stderr.String(), "Write the message") {
		t.Errorf("empty stdin: exit %d %q", code, stderr.String())
	}
	cmd, stdout, _ = command(t, "send", "+14155550123", "--api-base", stub(t))
	cmd.Stdin = strings.NewReader("Backup finished\n")
	if code := exitCode(cmd.Run()); code != 0 || !strings.Contains(stdout.String(), "Handed to your phone") {
		t.Errorf("piped text: exit %d %q", code, stdout.String())
	}
}

// Ctrl+C (SIGINT) while waiting ends with 130 and says the text is on its way; while watching, with 0.
func TestBinaryInterrupt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a console Ctrl+C can't be sent to one process from a test on Windows")
	}
	base := stub(t)
	for _, c := range []struct {
		args       []string
		ready      string
		code       int
		lastStderr string
	}{
		{[]string{"send", "+14155550123", "Hi", "--wait", "--api-base", base}, "Waiting up to 120 s", 130,
			"Stopped waiting. The text is still on its way; check it with: smstunnel status 000000000000000000000001\n"},
		{[]string{"received", "--watch", "--interval", "3", "--api-base", base}, "Watching for new texts", 0, "(Ctrl+C stops)\n"},
	} {
		cmd, _, stderr := command(t, c.args...)
		var buf safeWriter
		cmd.Stderr = &buf
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(buf.String(), c.ready) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		_ = cmd.Process.Signal(syscall.SIGINT)
		code := exitCode(cmd.Wait())
		_ = stderr
		if code != c.code || !strings.HasSuffix(buf.String(), c.lastStderr) {
			t.Errorf("%v: exit %d, stderr %q", c.args, code, buf.String())
		}
	}
}

type safeWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeWriter) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}
