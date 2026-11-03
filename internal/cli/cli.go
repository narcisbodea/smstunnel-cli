package cli

import (
	"errors"
	"io"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"runtime"
	"time"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
	"github.com/narcisbodea/smstunnel-cli/internal/term"
)

// Input is standard input.
type Input interface {
	io.Reader
	// IsTerminal reports whether it is a terminal (a console on Windows).
	IsTerminal() bool
	// Raw switches the terminal to raw mode: it returns a reader of the keys typed (UTF-8) and a function that
	// puts the terminal back.
	Raw() (io.Reader, func(), error)
}

// Env is everything the CLI takes from outside, so that it can run inside tests.
type Env struct {
	Args   []string
	Stdin  Input
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	GOOS   string
	Home   func() string
	Now    func() time.Time
	PID    int
	// Transport replaces the network connection (nil: the real one).
	Transport http.RoundTripper
	// Interrupts reports Ctrl+C while the CLI waits or watches; stop ends the reporting.
	Interrupts func() (ch <-chan struct{}, stop func())
	// Tools runs the programs of the system key stores.
	Tools toolRunner
}

// Main runs the CLI with this process's arguments, streams and environment, and returns the exit code.
func Main() int {
	args := make([]string, 0, len(os.Args))
	for _, a := range os.Args[1:] {
		args = append(args, js.DecodeUTF8([]byte(a)))
	}
	return Run(&Env{
		Args:       args,
		Stdin:      stdinFile{os.Stdin},
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Getenv:     func(name string) string { return js.DecodeUTF8([]byte(os.Getenv(name))) },
		GOOS:       runtime.GOOS,
		Home:       homeDir,
		Now:        time.Now,
		PID:        os.Getpid(),
		Interrupts: osInterrupts,
		Tools:      execTools{},
	})
}

type stdinFile struct{ f *os.File }

func (s stdinFile) Read(p []byte) (int, error)      { return s.f.Read(p) }
func (s stdinFile) IsTerminal() bool                { return term.IsTerminal(s.f) }
func (s stdinFile) Raw() (io.Reader, func(), error) { return term.Raw(s.f) }

// homeDir is os.homedir(): $HOME (%USERPROFILE% on Windows), else the account's home folder.
func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	return ""
}

func osInterrupts() (<-chan struct{}, func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	out := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-signals:
				select {
				case out <- struct{}{}:
				default:
				}
			case <-done:
				return
			}
		}
	}()
	return out, func() {
		signal.Stop(signals)
		close(done)
	}
}

var globalOptions = []optionSpec{
	{name: "json", isBool: true},
	{name: "api-base"},
	{name: "help", isBool: true, short: 'h'},
	{name: "version", isBool: true, short: 'v'},
}

var sendOptions = []optionSpec{
	{name: "phone"},
	{name: "country"},
	{name: "wait", isBool: true},
	{name: "timeout"},
	{name: "idempotency-key"},
}

var commandOptions = map[string][]optionSpec{
	"login":  nil,
	"logout": nil,
	"phones": nil,
	"send":   sendOptions,
	"code":   sendOptions,
	"status": nil,
	"received": {
		{name: "since"},
		{name: "from"},
		{name: "phone"},
		{name: "limit"},
		{name: "watch", isBool: true},
		{name: "interval"},
	},
	"usage": nil,
}

var commands = map[string]func(c *runContext, args []string) (int, error){
	"login":    login,
	"logout":   logout,
	"phones":   phones,
	"send":     func(c *runContext, args []string) (int, error) { return send(c, args, false) },
	"code":     func(c *runContext, args []string) (int, error) { return send(c, args, true) },
	"status":   status,
	"received": received,
	"usage":    usage,
}

func allOptions() []optionSpec {
	seen := map[string]bool{}
	var all []optionSpec
	add := func(specs []optionSpec) {
		for _, s := range specs {
			if !seen[s.name] {
				seen[s.name] = true
				all = append(all, s)
			}
		}
	}
	add(globalOptions)
	for _, name := range []string{"login", "logout", "phones", "send", "code", "status", "received", "usage"} {
		add(commandOptions[name])
	}
	return all
}

// Run runs the CLI and returns the exit code.
func Run(env *Env) int {
	jsonErrors := false
	for _, a := range env.Args {
		if a == "--json" {
			jsonErrors = true
		}
	}
	code, err := dispatch(env)
	if err != nil {
		return report(env, err, jsonErrors)
	}
	return code
}

func dispatch(env *Env) (int, error) {
	parsed, err := parseArgs(env.Args, allOptions())
	if err != nil {
		return 0, err
	}
	command := ""
	var args []string
	if len(parsed.positionals) > 0 {
		command, args = parsed.positionals[0], parsed.positionals[1:]
	}
	out := writer{env.Stdout}
	if parsed.flag("version") {
		out.write(Version + "\n")
		return exitOK, nil
	}
	if command == "help" {
		topic := ""
		if len(args) > 0 {
			topic = args[0]
		}
		out.write(helpFor(topic) + "\n")
		return exitOK, nil
	}
	if command == "" {
		out.write(mainHelp() + "\n")
		if parsed.flag("help") {
			return exitOK, nil
		}
		return exitUsage, nil
	}
	run, ok := commands[command]
	if !ok {
		return 0, usageError(`Unknown command "` + command + `". See "smstunnel --help".`)
	}
	if parsed.flag("help") {
		out.write(commandHelp[command] + "\n")
		return exitOK, nil
	}
	allowed := map[string]bool{}
	for _, s := range globalOptions {
		allowed[s.name] = true
	}
	for _, s := range commandOptions[command] {
		allowed[s.name] = true
	}
	for _, name := range parsed.order {
		if !allowed[name] {
			return 0, usageError("--" + name + ` is not an option of "smstunnel ` + command + `". See "smstunnel ` + command + ` --help".`)
		}
	}
	c, err := buildContext(env, parsed)
	if err != nil {
		return 0, err
	}
	return run(c, args)
}

type writer struct{ w io.Writer }

func (w writer) write(s string) { _, _ = io.WriteString(w.w, js.ForOutput(s)) }

func report(env *Env, err error, jsonErrors bool) int {
	stderr := writer{env.Stderr}
	var cliErr *CliError
	if errors.As(err, &cliErr) {
		if jsonErrors {
			e := js.NewObject().
				Set("message", js.StringValue(cliErr.Message)).
				Set("exitCode", js.IntValue(cliErr.ExitCode)).
				Set("status", cliErr.details.status).
				Set("code", cliErr.details.code).
				Set("permission", cliErr.details.permission).
				Set("retryAfter", cliErr.details.retryAfter).
				Set("messageId", cliErr.details.messageID)
			stderr.write(js.Stringify(js.NewObject().Set("error", e), "") + "\n")
		} else {
			stderr.write(cliErr.Message + "\n")
		}
		return cliErr.ExitCode
	}
	message := err.Error()
	if jsonErrors {
		e := js.NewObject().Set("message", js.StringValue(message)).Set("exitCode", js.IntValue(exitUnexpected))
		stderr.write(js.Stringify(js.NewObject().Set("error", e), "") + "\n")
	} else {
		var jsErr *jsError
		if errors.As(err, &jsErr) {
			stderr.write("Unexpected error: " + jsErr.name + ": " + jsErr.message + "\n")
		} else {
			stderr.write("Unexpected error: " + message + "\n")
		}
	}
	return exitUnexpected
}

// runContext is what every command gets: the API address, the options and the output.
type runContext struct {
	env           *Env
	json          bool
	apiBase       string
	apiBaseSource string
	opts          *parsedArgs
}

func buildContext(env *Env, opts *parsedArgs) (*runContext, error) {
	c := &runContext{env: env, json: opts.flag("json"), apiBase: defaultAPIBase, apiBaseSource: "default", opts: opts}
	if v, ok := opts.str("api-base"); ok {
		base, err := normalizeAPIBase(v)
		if err != nil {
			return nil, err
		}
		c.apiBase, c.apiBaseSource = base, "flag"
	} else if v := env.Getenv("SMSTUNNEL_API_BASE"); v != "" {
		base, err := normalizeAPIBase(v)
		if err != nil {
			return nil, err
		}
		c.apiBase, c.apiBaseSource = base, "env"
	} else {
		cfg, err := readConfig(env)
		if err != nil {
			return nil, err
		}
		if saved := cfg.Get("apiBase"); saved.Truthy() {
			if !saved.IsString() {
				return nil, &jsError{name: "TypeError", message: "input.trim is not a function"}
			}
			base, err := normalizeAPIBase(saved.Str())
			if err != nil {
				return nil, err
			}
			c.apiBase, c.apiBaseSource = base, "config"
		}
	}
	return c, nil
}

// normalizeAPIBase: https only (http only for this computer); no user name, password, query or fragment;
// "/api/v1" when there is no path.
func normalizeAPIBase(input string) (string, error) {
	u, err := js.ParseURL(js.Trim(input))
	if err != nil {
		return "", usageError(`"` + input + `" is not a URL. Use something like https://smstunnel.io/api/v1.`)
	}
	local := u.Host == "localhost" || u.Host == "127.0.0.1" || u.Host == "[::1]" || u.Host == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return "", usageError("The API address must start with https:// (http:// only for localhost).")
	}
	if u.HasUserinfo {
		return "", usageError("The API address may not contain a user name or password.")
	}
	if u.HasQuery || u.HasFragment {
		return "", usageError(`The API address may not contain "?" or "#".`)
	}
	path := u.Pathname
	for len(path) > 0 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	if path == "" {
		path = "/api/v1"
	}
	return u.Scheme + "://" + u.HostPort() + path, nil
}

func (c *runContext) line(s string) { writer{c.env.Stdout}.write(s + "\n") }
func (c *runContext) note(s string) { writer{c.env.Stderr}.write(s + "\n") }

// data prints a value as JSON.stringify(value, null, 2) does.
func (c *runContext) data(v js.Value) { writer{c.env.Stdout}.write(js.Stringify(v, "  ") + "\n") }

// jsonLine prints one compact JSON value per line (received --watch --json).
func (c *runContext) jsonLine(v js.Value) { writer{c.env.Stdout}.write(js.Stringify(v, "") + "\n") }

func (c *runContext) nowMs() float64 { return float64(c.env.Now().UnixMilli()) }

// pollInterval is the polling pace; SMSTUNNEL_POLL_INTERVAL_MS exists only so tests can run fast.
func (c *runContext) pollInterval(fallbackMs float64) float64 {
	v := c.env.Getenv("SMSTUNNEL_POLL_INTERVAL_MS")
	n := js.NaN
	if v != "" {
		n = js.ToNumber(v)
	}
	if !(n >= 50) || n > 1.7976931348623157e308 {
		return fallbackMs
	}
	return n
}
