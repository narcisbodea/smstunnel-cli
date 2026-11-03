package cli

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

func storeEnv(t *testing.T, vars map[string]string, goos string, tools toolRunner) *Env {
	dir := t.TempDir()
	all := map[string]string{"SMSTUNNEL_CONFIG_DIR": filepath.Join(dir, "cfg"), "SMSTUNNEL_KEY_STORE": "file", "PATH": ""}
	for k, v := range vars {
		if v == "\x00unset" {
			delete(all, k)
		} else {
			all[k] = v
		}
	}
	if tools == nil {
		tools = &fakeTools{}
	}
	return &Env{Getenv: func(n string) string { return all[n] }, GOOS: goos, Home: func() string { return dir }, PID: 7, Tools: tools}
}

func TestConfigDir(t *testing.T) {
	env := func(vars map[string]string, goos, home string) *Env {
		return &Env{Getenv: func(n string) string { return vars[n] }, GOOS: goos, Home: func() string { return home }}
	}
	if got := configDir(env(map[string]string{"SMSTUNNEL_CONFIG_DIR": "/x/y"}, "linux", "/home/a")); got != "/x/y" {
		t.Errorf("SMSTUNNEL_CONFIG_DIR: %q", got)
	}
	if got := configDir(env(map[string]string{"APPDATA": `C:\Users\a\AppData\Roaming`}, "windows", `C:\Users\a`)); got != filepath.Join(`C:\Users\a\AppData\Roaming`, "smstunnel") {
		t.Errorf("APPDATA: %q", got)
	}
	if got := configDir(env(map[string]string{}, "windows", `C:\Users\a`)); got != filepath.Join(`C:\Users\a`, "AppData", "Roaming", "smstunnel") {
		t.Errorf("no APPDATA: %q", got)
	}
	if got := configDir(env(map[string]string{"XDG_CONFIG_HOME": "/home/a/.cfg"}, "linux", "/home/a")); got != filepath.Join("/home/a/.cfg", "smstunnel") {
		t.Errorf("XDG: %q", got)
	}
	if got := configDir(env(map[string]string{}, "darwin", "/Users/a")); got != filepath.Join("/Users/a", ".config", "smstunnel") {
		t.Errorf("macOS: %q", got)
	}
}

func TestFileStore(t *testing.T) {
	env := storeEnv(t, nil, "linux", nil)
	saved, err := saveKey(env, keyHex, "")
	if err != nil || saved.kind != "file" || saved.fallbackReason != "SMSTUNNEL_KEY_STORE=file" || saved.label != configPath(env) {
		t.Fatalf("%+v %v", saved, err)
	}
	if got := readFile(t, configPath(env)); got != "{\n  \"keyStore\": \"file\",\n  \"apiKey\": \""+keyHex+"\"\n}\n" {
		t.Errorf("config.json: %q", got)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(configPath(env)); info.Mode().Perm() != 0o600 {
			t.Errorf("mode %v", info.Mode().Perm())
		}
		if info, _ := os.Stat(filepath.Dir(configPath(env))); info.Mode().Perm() != 0o700 {
			t.Errorf("folder mode %v", info.Mode().Perm())
		}
	}
	loaded, err := loadKey(env)
	if err != nil || loaded.key != keyHex || loaded.source != keyFromStored {
		t.Errorf("load: %+v %v", loaded, err)
	}
	if _, err := os.Stat(configPath(env) + ".7.tmp"); err == nil {
		t.Error("the temporary file is left")
	}
}

func TestSavedAPIBase(t *testing.T) {
	env := storeEnv(t, nil, "linux", nil)
	if _, err := saveKey(env, keyHex, "http://127.0.0.1:3121/api/v1"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := readConfig(env)
	if !cfg.Get("apiBase").StrictEquals("http://127.0.0.1:3121/api/v1") {
		t.Errorf("apiBase: %s", js.Stringify(cfg, ""))
	}
	_, _ = saveKey(env, keyHex, "")
	if cfg, _ := readConfig(env); !cfg.Get("apiBase").IsUndefined() {
		t.Errorf("apiBase kept: %s", js.Stringify(cfg, ""))
	}
}

func TestFallbacks(t *testing.T) {
	env := storeEnv(t, map[string]string{"SMSTUNNEL_KEY_STORE": "\x00unset"}, "aix", nil)
	if saved, err := saveKey(env, keyHex, ""); err != nil || saved.kind != "file" || !strings.Contains(saved.fallbackReason, "no system keychain") {
		t.Errorf("unknown system: %+v %v", saved, err)
	}
	env = storeEnv(t, map[string]string{"SMSTUNNEL_KEY_STORE": "\x00unset"}, "linux", nil)
	store, err := systemStore(env, "linux")
	if err != nil || store.kind() != storeSecret || store.available() {
		t.Fatalf("secret service: %v %v", store, err)
	}
	if saved, _ := saveKey(env, keyHex, ""); saved.kind != "file" || saved.fallbackReason != "your system keyring (Secret Service) is not available here" {
		t.Errorf("linux: %+v", saved)
	}
}

func TestEnvKeyWins(t *testing.T) {
	env := storeEnv(t, nil, "linux", nil)
	_, _ = saveKey(env, keyHex, "")
	other := "sk_" + "live_" + strings.Repeat("f", 32)
	vars := map[string]string{"SMSTUNNEL_CONFIG_DIR": configDir(env), "SMSTUNNEL_API_KEY": " " + other + " "}
	env2 := &Env{Getenv: func(n string) string { return vars[n] }, GOOS: "linux", Home: env.Home}
	if loaded, _ := loadKey(env2); loaded.key != other || loaded.source != keyFromEnv {
		t.Errorf("%+v", loaded)
	}
}

func TestNoKeyAndLogout(t *testing.T) {
	env := storeEnv(t, nil, "linux", nil)
	if loaded, err := loadKey(env); loaded != nil || err != nil {
		t.Errorf("no key: %+v %v", loaded, err)
	}
	if cfg, _ := readConfig(env); js.Stringify(cfg, "") != "{}" {
		t.Errorf("config: %s", js.Stringify(cfg, ""))
	}
	_, _ = saveKey(env, keyHex, "")
	removed, err := removeKey(env)
	if err != nil || len(removed) != 1 || removed[0] != configPath(env) || fileExists(configPath(env)) {
		t.Errorf("logout: %v %v", removed, err)
	}
	if removed, _ := removeKey(env); len(removed) != 0 {
		t.Errorf("second logout: %v", removed)
	}
}

func TestRefusedKeys(t *testing.T) {
	env := storeEnv(t, nil, "linux", nil)
	for _, bad := range []string{`sk_` + `live_"; rm -rf`, "short", "sk live with spaces", "sk_" + "live_" + strings.Repeat("a", 300)} {
		if _, err := saveKey(env, bad, ""); err == nil || err.(*CliError).ExitCode != 2 {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if fileExists(configPath(env)) {
		t.Error("written")
	}
}

func TestBrokenConfig(t *testing.T) {
	env := storeEnv(t, nil, "linux", nil)
	_, _ = writeConfig(env, "file", "", keyHex)
	_ = os.WriteFile(configPath(env), []byte("{ not json"), 0o600)
	_, err := readConfig(env)
	if e, ok := err.(*CliError); !ok || e.ExitCode != 1 || !strings.Contains(e.Message, "not valid JSON") {
		t.Errorf("%v", err)
	}
	// A byte-order mark is not JSON either (JSON.parse refuses it).
	_ = os.WriteFile(configPath(env), []byte("\xef\xbb\xbf{}"), 0o600)
	if _, err := readConfig(env); err == nil {
		t.Error("BOM taken")
	}
	_ = os.WriteFile(configPath(env), []byte("[1]"), 0o600)
	if cfg, err := readConfig(env); err != nil || !cfg.Get("apiKey").IsUndefined() {
		t.Errorf("array: %v", err)
	}
}

func TestKeyStoreSettings(t *testing.T) {
	if _, err := systemStore(storeEnv(t, map[string]string{"SMSTUNNEL_KEY_STORE": "cloud"}, "linux", nil), "linux"); err == nil || err.(*CliError).ExitCode != 2 {
		t.Errorf("cloud: %v", err)
	}
	if s, err := systemStore(storeEnv(t, map[string]string{"SMSTUNNEL_KEY_STORE": "FILE"}, "darwin", nil), "darwin"); s != nil || err != nil {
		t.Errorf("FILE: %v %v", s, err)
	}
	auto := storeEnv(t, map[string]string{"SMSTUNNEL_KEY_STORE": "\x00unset"}, "darwin", nil)
	if s, _ := systemStore(auto, "darwin"); s.kind() != storeMacOS {
		t.Error("darwin")
	}
	if s, _ := systemStore(auto, "windows"); s.kind() != storeWindows {
		t.Error("windows")
	}
	for _, goos := range []string{"linux", "freebsd", "openbsd"} {
		if s, _ := systemStore(auto, goos); s.kind() != storeSecret {
			t.Error(goos)
		}
	}
	for _, goos := range []string{"netbsd", "android", "aix"} {
		if s, _ := systemStore(auto, goos); s != nil {
			t.Error(goos)
		}
	}
	bad := storeEnv(t, map[string]string{"SMSTUNNEL_KEY_STORE": "\x00unset", "SMSTUNNEL_KEYCHAIN_SERVICE": "bad'name"}, "darwin", nil)
	if _, err := systemStore(bad, "darwin"); err == nil || err.(*CliError).ExitCode != 2 {
		t.Errorf("bad service: %v", err)
	}
}

// The system stores run the same programs, with the same arguments and input, as the Node version.
func TestSystemStoreCommands(t *testing.T) {
	secrets := map[string]string{}
	tools := &fakeTools{}
	tools.answer = func(c toolCall) (int, string, string) {
		switch {
		case c.name == securityTool && c.args[0] == "-i":
			secrets["mac"] = c.stdin[strings.Index(c.stdin, `-w "`)+4 : len(c.stdin)-2]
			return 0, "", ""
		case c.name == securityTool && c.args[0] == "find-generic-password":
			if k, ok := secrets["mac"]; ok {
				return 0, k + "\n", ""
			}
			return 44, "", "security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain."
		case c.name == securityTool && c.args[0] == "delete-generic-password":
			_, ok := secrets["mac"]
			delete(secrets, "mac")
			if ok {
				return 0, "", ""
			}
			return 44, "", ""
		}
		return -1, "", ""
	}
	env := storeEnv(t, map[string]string{"SMSTUNNEL_KEY_STORE": "auto", "SMSTUNNEL_KEYCHAIN_SERVICE": "smstunnel-test"}, "darwin", tools)
	mac := &macosKeychain{storeBase{env: env, service: "smstunnel-test"}}
	if err := mac.save(keyHex); err != nil {
		t.Fatal(err)
	}
	want := `add-generic-password -U -a "api-key" -s "smstunnel-test" -l "SMSTunnel API key" -w "` + keyHex + `"` + "\n"
	if tools.calls[0].stdin != want || strings.Join(tools.calls[1].args, " ") != "find-generic-password -a api-key -s smstunnel-test -w" {
		t.Errorf("calls: %+v", tools.calls[:2])
	}
	for _, c := range tools.calls {
		if strings.Contains(strings.Join(c.args, " "), keyHex) {
			t.Error("the key in the arguments")
		}
	}
	if k, ok := mac.load(); !ok || k != keyHex || !mac.remove() || mac.remove() {
		t.Error("load/remove")
	}
	if _, ok := mac.load(); ok {
		t.Error("still there")
	}

	tools.calls = nil
	tools.answer = func(c toolCall) (int, string, string) { return 1, "", "secret-tool: no such secret collection\n" }
	ss := &secretService{storeBase{env: env, service: "smstunnel"}}
	if err := ss.save(keyHex); err == nil || err.Error() != "secret-tool: no such secret collection" {
		t.Errorf("secret-tool error: %v", err)
	}
	if c := tools.calls[0]; c.name != "secret-tool" || strings.Join(c.args, "|") != "store|--label=SMSTunnel API key|service|smstunnel|account|api-key" || c.stdin != keyHex {
		t.Errorf("store call: %+v", c)
	}

	tools.calls = nil
	var vault string
	tools.answer = func(c toolCall) (int, string, string) {
		script := decodeEncodedCommand(t, c.args[len(c.args)-1])
		switch {
		case strings.Contains(script, "$vault.Add("):
			vault = strings.TrimSpace(c.stdin)
			return 0, "", ""
		case strings.Contains(script, "Retrieve("):
			if vault == "" {
				return 3, "", ""
			}
			return 0, vault, ""
		case strings.Contains(script, "$n++"):
			n := "0"
			if vault != "" {
				n = "1"
			}
			vault = ""
			return 0, n, ""
		}
		return 1, "", "unknown\r\nsecond line"
	}
	winEnv := storeEnv(t, map[string]string{"SystemRoot": `C:\Windows`}, "windows", tools)
	win := &windowsCredentialManager{storeBase{env: winEnv, service: "smstunnel"}}
	if err := win.save(keyHex); err != nil {
		t.Fatal(err)
	}
	c := tools.calls[0]
	if c.name != filepath.Join(`C:\Windows`, "System32", "WindowsPowerShell", "v1.0", "powershell.exe") ||
		strings.Join(c.args[:4], " ") != "-NoLogo -NoProfile -NonInteractive -EncodedCommand" || c.stdin != keyHex {
		t.Errorf("powershell call: %+v", c)
	}
	script := decodeEncodedCommand(t, c.args[4])
	if !strings.HasPrefix(script, "$ErrorActionPreference = 'Stop'; $null = [Windows.Security.Credentials.PasswordVault, Windows.Security.Credentials, ContentType = WindowsRuntime]; $vault = New-Object Windows.Security.Credentials.PasswordVault; $resource = 'smstunnel'; $account = 'api-key'; $secret = [Console]::In.ReadToEnd().Trim(); ") {
		t.Errorf("script: %s", script)
	}
	if k, ok := win.load(); !ok || k != keyHex || !win.remove() || win.remove() {
		t.Error("vault load/remove")
	}
}

func decodeEncodedCommand(t *testing.T, b64 string) string {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	return string(utf16.Decode(units))
}

// A key saved in a system store: config.json names the store, the key is not in it; logout removes both.
func TestSystemStoreLoginAndLogout(t *testing.T) {
	secrets := map[string]string{}
	tools := &fakeTools{answer: func(c toolCall) (int, string, string) {
		switch c.args[0] {
		case "store":
			secrets["s"] = c.stdin
			return 0, "", ""
		case "lookup":
			if k, ok := secrets["s"]; ok {
				return 0, k, ""
			}
			return 1, "", ""
		case "clear":
			delete(secrets, "s")
			return 0, "", ""
		}
		return -1, "", ""
	}}
	bin := t.TempDir()
	_ = os.WriteFile(filepath.Join(bin, "secret-tool"), []byte("#!/bin/sh\n"), 0o755)
	env := storeEnv(t, map[string]string{"SMSTUNNEL_KEY_STORE": "\x00unset", "PATH": bin, "DBUS_SESSION_BUS_ADDRESS": "unix:path=/x"}, "linux", tools)
	if runtime.GOOS == "windows" {
		t.Skip("secret-tool's executable bit is a POSIX check")
	}
	saved, err := saveKey(env, keyHex, "http://127.0.0.1:3121/api/v1")
	if err != nil || saved.kind != storeSecret || saved.label != "your system keyring (Secret Service)" {
		t.Fatalf("%+v %v", saved, err)
	}
	if got := readFile(t, configPath(env)); got != "{\n  \"keyStore\": \"secret-service\",\n  \"apiBase\": \"http://127.0.0.1:3121/api/v1\"\n}\n" {
		t.Errorf("config.json: %q", got)
	}
	if loaded, _ := loadKey(env); loaded == nil || loaded.key != keyHex {
		t.Errorf("load: %+v", loaded)
	}
	delete(secrets, "s")
	if _, err := loadKey(env); err == nil || err.Error() != `The saved key is missing from your system keyring (Secret Service). Run "smstunnel login" again.` || err.(*CliError).ExitCode != 3 {
		t.Errorf("missing: %v", err)
	}
	secrets["s"] = keyHex
	removed, _ := removeKey(env)
	if len(removed) != 1 || removed[0] != "your system keyring (Secret Service)" || fileExists(configPath(env)) {
		t.Errorf("logout: %v", removed)
	}
}

// config.json written by the Node version is read as it is, and the other way round.
func TestNodeWrittenConfig(t *testing.T) {
	env := storeEnv(t, nil, "linux", nil)
	_ = os.MkdirAll(configDir(env), 0o700)
	_ = os.WriteFile(configPath(env), []byte("{\n  \"keyStore\": \"file\",\n  \"apiBase\": \"http://127.0.0.1:3141/api/v1\",\n  \"apiKey\": \""+keyHex+"\"\n}\n"), 0o600)
	if loaded, err := loadKey(env); err != nil || loaded.key != keyHex {
		t.Errorf("%+v %v", loaded, err)
	}
	c, err := buildContext(env, &parsedArgs{options: map[string]option{}})
	if err != nil || c.apiBase != "http://127.0.0.1:3141/api/v1" || c.apiBaseSource != "config" {
		t.Errorf("api base from config.json: %+v %v", c, err)
	}
	// No keyStore but an apiKey: the file's key (an older login).
	_ = os.WriteFile(configPath(env), []byte(`{"apiKey":"`+keyHex+`"}`), 0o600)
	if loaded, _ := loadKey(env); loaded == nil || loaded.key != keyHex {
		t.Error("apiKey without keyStore")
	}
	_ = os.WriteFile(configPath(env), []byte(`{"keyStore":"file"}`), 0o600)
	if loaded, _ := loadKey(env); loaded != nil {
		t.Error("file store without a key")
	}
	_ = os.WriteFile(configPath(env), []byte(`{"apiBase":5}`), 0o600)
	if _, err := buildContext(env, &parsedArgs{options: map[string]option{}}); err == nil || err.Error() != "input.trim is not a function" {
		t.Errorf("number apiBase: %v", err)
	}
}
