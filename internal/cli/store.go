package cli

import (
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

// The saved key: config.json (the same file, folder and format as the Node version's), and the system store it
// names. Both versions read each other's login.

const (
	storeMacOS   = "macos-keychain"
	storeWindows = "windows-credential-manager"
	storeSecret  = "secret-service"
	storeFile    = "file"

	keyAccount = "api-key"
	keyLabel   = "SMSTunnel API key"
)

// keyPattern is /^[A-Za-z0-9_.-]{8,256}$/: keys are sk_live_ + hex, and nothing else is safe to hand to the tools.
func keyPattern(key string) bool {
	if len(key) < 8 || len(key) > 256 {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

// configDir: SMSTUNNEL_CONFIG_DIR, else %APPDATA%\smstunnel on Windows, else $XDG_CONFIG_HOME/smstunnel or
// ~/.config/smstunnel (macOS included).
func configDir(env *Env) string {
	if d := env.Getenv("SMSTUNNEL_CONFIG_DIR"); d != "" {
		return d
	}
	if env.GOOS == "windows" {
		base := env.Getenv("APPDATA")
		if base == "" {
			base = filepath.Join(env.Home(), "AppData", "Roaming")
		}
		return filepath.Join(base, "smstunnel")
	}
	base := env.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(env.Home(), ".config")
	}
	return filepath.Join(base, "smstunnel")
}

func configPath(env *Env) string { return filepath.Join(configDir(env), "config.json") }

// readConfig reads config.json: {} when it is missing or not an object.
func readConfig(env *Env) (js.Value, error) {
	file := configPath(env)
	data, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return js.NewObject(), nil
		}
		return js.UndefinedValue, &CliError{Message: "Could not read " + file + ": " + nodeFSMessage(err, "open", file), ExitCode: exitUnexpected}
	}
	v, err := js.Parse(js.DecodeUTF8(data))
	if err != nil {
		return js.UndefinedValue, &CliError{Message: file + ` is not valid JSON. Delete it and run "smstunnel login" again.`, ExitCode: exitUnexpected}
	}
	if v.Kind() != js.Object && v.Kind() != js.Array {
		return js.NewObject(), nil
	}
	return v, nil
}

// writeConfig writes config.json readable and writable only by you (0600), in a folder only you can open (0700).
func writeConfig(env *Env, keyStore, apiBase, apiKey string) (string, error) {
	dir := configDir(env)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", &jsError{name: "Error", message: nodeFSMessage(err, "mkdir", dir)}
	}
	_ = os.Chmod(dir, 0o700) // not ours to change (a shared folder): the file mode still applies
	file := filepath.Join(dir, "config.json")
	temp := file + "." + strconv.Itoa(env.PID) + ".tmp"
	clean := js.NewObject()
	if keyStore != "" {
		clean.Set("keyStore", js.StringValue(keyStore))
	}
	if apiBase != "" {
		clean.Set("apiBase", js.StringValue(apiBase))
	}
	if apiKey != "" {
		clean.Set("apiKey", js.StringValue(apiKey))
	}
	if err := os.WriteFile(temp, []byte(js.Stringify(clean, "  ")+"\n"), 0o600); err != nil {
		return "", &jsError{name: "Error", message: nodeFSMessage(err, "open", temp)}
	}
	if err := os.Chmod(temp, 0o600); err != nil {
		return "", &jsError{name: "Error", message: nodeFSMessage(err, "chmod", temp)}
	}
	if err := os.Rename(temp, file); err != nil {
		return "", &jsError{name: "Error", message: nodeFSMessage(err, "rename", temp) + " -> '" + file + "'"}
	}
	return file, nil
}

// nodeFSMessage words a file error the way Node.js does: "EACCES: permission denied, open '/path'".
func nodeFSMessage(err error, syscallName, path string) string {
	code, words := "", ""
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		path = pathErr.Path
	}
	if info, statErr := os.Stat(path); statErr == nil && info.IsDir() && syscallName == "open" {
		return "EISDIR: illegal operation on a directory, read"
	}
	switch {
	case errors.Is(err, fs.ErrPermission):
		code, words = "EACCES", "permission denied"
	case errors.Is(err, fs.ErrNotExist):
		code, words = "ENOENT", "no such file or directory"
	case strings.Contains(err.Error(), "not a directory"):
		code, words = "ENOTDIR", "not a directory"
	case strings.Contains(err.Error(), "is a directory"):
		return "EISDIR: illegal operation on a directory, read"
	default:
		return err.Error()
	}
	return code + ": " + words + ", " + syscallName + " '" + path + "'"
}

// keychainService is the service (resource) name in the system store.
func keychainService(env *Env) (string, error) {
	name := env.Getenv("SMSTUNNEL_KEYCHAIN_SERVICE")
	if name == "" {
		name = "smstunnel"
	}
	ok := len(name) >= 1 && len(name) <= 64
	for i := 0; ok && i < len(name); i++ {
		c := name[i]
		ok = c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
	}
	if !ok {
		return "", usageError(`SMSTUNNEL_KEYCHAIN_SERVICE may only hold letters, digits, ".", "_" and "-".`)
	}
	return name, nil
}

// secretStore is a system store for the key.
type secretStore interface {
	kind() string
	label() string
	available() bool
	save(key string) error
	load() (string, bool)
	remove() bool
}

type storeBase struct {
	env     *Env
	service string
}

// systemStore is the store login tries on this system, or nil (the key then goes in config.json).
func systemStore(env *Env, goos string) (secretStore, error) {
	choice := env.Getenv("SMSTUNNEL_KEY_STORE")
	if choice == "" {
		choice = "auto"
	}
	choice = js.ToLower(choice)
	if choice == "file" {
		return nil, nil
	}
	if choice != "auto" {
		return nil, usageError(`SMSTUNNEL_KEY_STORE takes "auto" or "file".`)
	}
	switch goos {
	case "darwin":
		return storeOfKind(env, storeMacOS)
	case "windows":
		return storeOfKind(env, storeWindows)
	case "linux", "freebsd", "openbsd":
		return storeOfKind(env, storeSecret)
	}
	return nil, nil
}

func storeOfKind(env *Env, kind string) (secretStore, error) {
	switch kind {
	case storeMacOS, storeWindows, storeSecret:
	default:
		return nil, nil
	}
	service, err := keychainService(env)
	if err != nil {
		return nil, err
	}
	b := storeBase{env: env, service: service}
	switch kind {
	case storeMacOS:
		return &macosKeychain{b}, nil
	case storeWindows:
		return &windowsCredentialManager{b}, nil
	}
	return &secretService{b}, nil
}

func trimmedOr(text, fallback string) string {
	if text == "" {
		text = fallback
	}
	return js.Trim(text)
}

// macosKeychain is the macOS Keychain through /usr/bin/security; the key goes in on stdin (security -i).
type macosKeychain struct{ storeBase }

const securityTool = "/usr/bin/security"

func (s *macosKeychain) kind() string    { return storeMacOS }
func (s *macosKeychain) label() string   { return "your macOS Keychain" }
func (s *macosKeychain) available() bool { return fileExists(securityTool) }

func (s *macosKeychain) save(key string) error {
	status, stdout, stderr := s.env.Tools.run(securityTool, []string{"-i"},
		`add-generic-password -U -a "`+keyAccount+`" -s "`+s.service+`" -l "`+keyLabel+`" -w "`+key+`"`+"\n")
	if loaded, _ := s.load(); status != 0 || loaded != key {
		text := stderr
		if text == "" {
			text = stdout
		}
		return errors.New(trimmedOr(text, "security could not save the key"))
	}
	return nil
}

func (s *macosKeychain) load() (string, bool) {
	status, stdout, _ := s.env.Tools.run(securityTool, []string{"find-generic-password", "-a", keyAccount, "-s", s.service, "-w"}, "")
	if status != 0 {
		return "", false
	}
	key := js.Trim(stdout)
	return key, key != ""
}

func (s *macosKeychain) remove() bool {
	status, _, _ := s.env.Tools.run(securityTool, []string{"delete-generic-password", "-a", keyAccount, "-s", s.service}, "")
	return status == 0
}

// secretService is the Secret Service (GNOME Keyring, KWallet) through secret-tool, which reads the key on stdin.
type secretService struct{ storeBase }

func (s *secretService) kind() string  { return storeSecret }
func (s *secretService) label() string { return "your system keyring (Secret Service)" }
func (s *secretService) available() bool {
	return onPath("secret-tool", s.env.Getenv("PATH")) && s.env.Getenv("DBUS_SESSION_BUS_ADDRESS") != ""
}

func (s *secretService) save(key string) error {
	status, _, stderr := s.env.Tools.run("secret-tool", []string{"store", "--label=" + keyLabel, "service", s.service, "account", keyAccount}, key)
	if loaded, _ := s.load(); status != 0 || loaded != key {
		return errors.New(trimmedOr(stderr, "secret-tool could not save the key"))
	}
	return nil
}

func (s *secretService) load() (string, bool) {
	status, stdout, _ := s.env.Tools.run("secret-tool", []string{"lookup", "service", s.service, "account", keyAccount}, "")
	if status != 0 {
		return "", false
	}
	key := js.Trim(stdout)
	return key, key != ""
}

func (s *secretService) remove() bool {
	status, _, _ := s.env.Tools.run("secret-tool", []string{"clear", "service", s.service, "account", keyAccount}, "")
	return status == 0
}

// windowsCredentialManager is the PasswordVault of Windows PowerShell 5.1 (it shows under Web Credentials in
// Credential Manager), with the same entry and the same scripts as the Node version: resource <service>, user
// name api-key. The script goes in -EncodedCommand, the key only through stdin.
type windowsCredentialManager struct{ storeBase }

func (s *windowsCredentialManager) kind() string  { return storeWindows }
func (s *windowsCredentialManager) label() string { return "Windows Credential Manager" }

func (s *windowsCredentialManager) powershell() string {
	root := s.env.Getenv("SystemRoot")
	if root == "" {
		root = s.env.Getenv("SYSTEMROOT")
	}
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
}

func (s *windowsCredentialManager) available() bool { return fileExists(s.powershell()) }

func (s *windowsCredentialManager) run(script, input string) (int, string, string) {
	prelude := "$ErrorActionPreference = 'Stop'; " +
		"$null = [Windows.Security.Credentials.PasswordVault, Windows.Security.Credentials, ContentType = WindowsRuntime]; " +
		"$vault = New-Object Windows.Security.Credentials.PasswordVault; " +
		"$resource = '" + s.service + "'; $account = '" + keyAccount + "'; "
	units := utf16.Encode([]rune(prelude + script))
	raw := make([]byte, 0, len(units)*2)
	for _, u := range units {
		raw = append(raw, byte(u), byte(u>>8))
	}
	return s.env.Tools.run(s.powershell(),
		[]string{"-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(raw)}, input)
}

func (s *windowsCredentialManager) save(key string) error {
	status, _, stderr := s.run("$secret = [Console]::In.ReadToEnd().Trim(); "+
		"try { $vault.FindAllByResource($resource) | ForEach-Object { $vault.Remove($_) } } catch { } "+
		"$vault.Add((New-Object Windows.Security.Credentials.PasswordCredential($resource, $account, $secret)))", key)
	if loaded, _ := s.load(); status != 0 || loaded != key {
		first, _, _ := strings.Cut(trimmedOr(stderr, "PowerShell could not save the key"), "\n")
		return errors.New(first)
	}
	return nil
}

func (s *windowsCredentialManager) load() (string, bool) {
	status, stdout, _ := s.run("try { $c = $vault.Retrieve($resource, $account) } catch { exit 3 }; $c.RetrievePassword(); [Console]::Out.Write($c.Password)", "")
	if status != 0 {
		return "", false
	}
	key := js.Trim(stdout)
	return key, key != ""
}

func (s *windowsCredentialManager) remove() bool {
	status, stdout, _ := s.run("$n = 0; try { $vault.FindAllByResource($resource) | ForEach-Object { $vault.Remove($_); $n++ } } catch { }; [Console]::Out.Write($n)", "")
	return status == 0 && js.ToNumber(stdout) > 0
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// onPath reports whether an executable of that name is in one of PATH's folders.
func onPath(command, pathList string) bool {
	for _, dir := range strings.Split(pathList, string(os.PathListSeparator)) {
		if dir != "" && executable(filepath.Join(dir, command)) {
			return true
		}
	}
	return false
}

type saveResult struct {
	kind           string
	label          string
	fallbackReason string
}

// saveKey saves the key in the system store when there is one that works, else in config.json (0600).
func saveKey(env *Env, key, apiBase string) (*saveResult, error) {
	if !keyPattern(key) {
		return nil, usageError("That does not look like an SMSTunnel API key (they look like sk_live_…).")
	}
	previous, err := readConfig(env)
	if err != nil {
		return nil, err
	}
	store, err := systemStore(env, env.GOOS)
	if err != nil {
		return nil, err
	}
	reason := ""
	switch {
	case store != nil && store.available():
		if err := store.save(key); err != nil {
			message := err.Error()
			if message == "" {
				message = "unknown error"
			}
			reason = store.label() + " did not take it (" + message + ")"
		} else if _, err := writeConfig(env, store.kind(), apiBase, ""); err != nil {
			reason = store.label() + " did not take it (" + err.Error() + ")"
		} else {
			return &saveResult{kind: store.kind(), label: store.label()}, nil
		}
	case store != nil:
		reason = store.label() + " is not available here"
	case js.ToLower(env.Getenv("SMSTUNNEL_KEY_STORE")) == "file":
		reason = "SMSTUNNEL_KEY_STORE=file"
	default:
		reason = "no system keychain is known on this system"
	}
	// The earlier system entry, if any, is removed so only one copy of a key stays around.
	if kind := previous.Get("keyStore"); kind.Truthy() && !kind.StrictEquals(storeFile) && kind.IsString() {
		if old, err := storeOfKind(env, kind.Str()); err == nil && old != nil {
			old.remove()
		}
	}
	file, err := writeConfig(env, storeFile, apiBase, key)
	if err != nil {
		return nil, err
	}
	return &saveResult{kind: storeFile, label: file, fallbackReason: reason}, nil
}

type loadedKey struct {
	key    string
	source keySource
}

// loadKey: SMSTUNNEL_API_KEY wins; otherwise the saved key. nil when there is none.
func loadKey(env *Env) (*loadedKey, error) {
	if fromEnv := js.Trim(env.Getenv("SMSTUNNEL_API_KEY")); fromEnv != "" {
		return &loadedKey{key: fromEnv, source: keyFromEnv}, nil
	}
	cfg, err := readConfig(env)
	if err != nil {
		return nil, err
	}
	kind, apiKey := cfg.Get("keyStore"), cfg.Get("apiKey")
	if kind.StrictEquals(storeFile) || (!kind.Truthy() && apiKey.Truthy()) {
		if !apiKey.Truthy() {
			return nil, nil
		}
		return &loadedKey{key: apiKey.String(), source: keyFromStored}, nil
	}
	if !kind.IsString() {
		return nil, nil
	}
	store, err := storeOfKind(env, kind.Str())
	if err != nil || store == nil {
		return nil, err
	}
	key, ok := store.load()
	if !ok {
		return nil, &CliError{Message: "The saved key is missing from " + store.label() + `. Run "smstunnel login" again.`, ExitCode: exitAuth}
	}
	return &loadedKey{key: key, source: keyFromStored}, nil
}

// removeKey removes the saved key wherever it is, and the API address saved with it.
func removeKey(env *Env) ([]string, error) {
	cfg, err := readConfig(env)
	if err != nil {
		return nil, err
	}
	var removed []string
	if kind := cfg.Get("keyStore"); kind.IsString() {
		store, err := storeOfKind(env, kind.Str())
		if err != nil {
			return nil, err
		}
		if store != nil && store.remove() {
			removed = append(removed, store.label())
		}
	}
	file := configPath(env)
	if cfg.Get("apiKey").Truthy() {
		removed = append(removed, file)
	}
	if fileExists(file) {
		_ = os.Remove(file)
	}
	return removed, nil
}
