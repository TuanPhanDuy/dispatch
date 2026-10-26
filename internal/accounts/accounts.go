// Package accounts lets sessions run under named accounts, so the same
// agent CLI can be signed in to different accounts side by side.
//
// An account is a directory. Inside it, each supported tool gets its own
// configuration home, pointed at through the variable that tool reads:
//
//	<account>/claude  CLAUDE_CONFIG_DIR  (Claude Code; default ~/.claude)
//	<account>/codex   CODEX_HOME         (Codex CLI;  default ~/.codex)
//
// Both tools keep their login inside that home, or — when they use the OS
// keychain — under a keychain key derived from the home's path, so two
// accounts never share a login. Every session under an account gets all of
// these variables, whatever its command: a shell opened under "work" runs
// `claude` and `codex` as "work" too.
//
// For tools without a built-in entry, an account's optional env file adds
// arbitrary KEY=VALUE pairs (API keys, another tool's home variable, ...).
//
// The daemon is the only caller that resolves names to directories, so the
// TUI and the GUI always agree on where an account lives regardless of OS.
package accounts

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/adrg/xdg"
)

// Tool is a CLI whose configuration home an account isolates.
type Tool struct {
	Name   string // subdirectory of the account
	EnvVar string // variable the tool reads its home from
}

// Tools are the CLIs every account isolates.
var Tools = []Tool{
	{Name: "claude", EnvVar: "CLAUDE_CONFIG_DIR"},
	{Name: "codex", EnvVar: "CODEX_HOME"},
}

// EnvFileName is the per-account file of extra KEY=VALUE variables.
const EnvFileName = "env"

// clearedVars must not leak from the daemon's own environment into an
// account session: CLAUDE_SECURESTORAGE_CONFIG_DIR overrides which
// directory Claude Code derives its keychain entry from, which would make
// every account share one login.
var clearedVars = []string{"CLAUDE_SECURESTORAGE_CONFIG_DIR"}

var (
	validName   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)
	validEnvKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Validate reports whether name can be used as an account name: 1–40
// letters, digits, '.', '_' or '-', starting with a letter or digit (so it
// is always a single, non-hidden path component).
func Validate(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid account name %q: use 1-40 letters, digits, '.', '_' or '-', starting with a letter or digit", name)
	}
	return nil
}

// RootEnv overrides Root, mainly so tests and throwaway daemons don't touch
// the user's real accounts.
const RootEnv = "DISPATCH_ACCOUNTS_DIR"

// Root is the directory holding one subdirectory per account:
// ~/Library/Application Support/dispatch/accounts on macOS,
// $XDG_DATA_HOME/dispatch/accounts (~/.local/share/...) on Linux.
func Root() string {
	if dir := os.Getenv(RootEnv); dir != "" {
		return dir
	}
	return filepath.Join(xdg.DataHome, "dispatch", "accounts")
}

// envFileTemplate is written into every new account so the file is easy
// to find; it sets nothing until a line is uncommented.
const envFileTemplate = `# Extra environment variables for every session under this account,
# one KEY=VALUE per line. Lines starting with # are ignored.
#
# API keys for tools that log in with one, e.g. Aider:
#   OPENAI_API_KEY=sk-...
#   ANTHROPIC_API_KEY=sk-ant-...
#
# Another tool's configuration home, to keep its login per account:
#   GEMINI_CLI_HOME=/path/to/this/account/gemini
`

// Dir returns account name's directory, creating it and each tool's home
// inside it (all private to the user) as needed.
func Dir(name string) (string, error) {
	if err := Validate(name); err != nil {
		return "", err
	}
	dir := filepath.Join(Root(), name)
	for _, t := range Tools {
		if err := os.MkdirAll(filepath.Join(dir, t.Name), 0o700); err != nil {
			return "", fmt.Errorf("create account %q: %w", name, err)
		}
	}
	envFile := filepath.Join(dir, EnvFileName)
	if _, err := os.Stat(envFile); os.IsNotExist(err) {
		if err := os.WriteFile(envFile, []byte(envFileTemplate), 0o600); err != nil {
			return "", fmt.Errorf("create account %q: %w", name, err)
		}
	}
	return dir, nil
}

// Env returns base with account name's variables applied: each tool's
// home, then the account's env file, each replacing any inherited value.
func Env(base []string, name string) ([]string, error) {
	dir, err := Dir(name)
	if err != nil {
		return nil, err
	}
	extra, err := readEnvFile(filepath.Join(dir, EnvFileName))
	if err != nil {
		return nil, fmt.Errorf("account %q: %w", name, err)
	}

	set := make([]string, 0, len(Tools)+len(extra))
	for _, t := range Tools {
		set = append(set, t.EnvVar+"="+filepath.Join(dir, t.Name))
	}
	set = append(set, extra...)

	drop := map[string]bool{}
	for _, k := range clearedVars {
		drop[k] = true
	}
	for _, kv := range set {
		drop[kv[:strings.IndexByte(kv, '=')]] = true
	}
	env := make([]string, 0, len(base)+len(set))
	for _, kv := range base {
		if i := strings.IndexByte(kv, '='); i >= 0 && drop[kv[:i]] {
			continue
		}
		env = append(env, kv)
	}
	// Later entries win, so an env file line can override a tool home.
	seen := map[string]int{}
	for _, kv := range set {
		k := kv[:strings.IndexByte(kv, '=')]
		if i, ok := seen[k]; ok {
			env[i] = kv
			continue
		}
		seen[k] = len(env)
		env = append(env, kv)
	}
	return env, nil
}

// readEnvFile parses KEY=VALUE lines, ignoring blanks and # comments and
// accepting an optional leading "export " and matching surrounding quotes.
// A missing file sets nothing.
func readEnvFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []string
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || !validEnvKey.MatchString(k) {
			return nil, fmt.Errorf("%s line %d: expected KEY=VALUE", EnvFileName, n)
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out = append(out, k+"="+v)
	}
	return out, sc.Err()
}

// SessionEnv is the environment for a session under account: nil (inherit
// the caller's own) for no account, otherwise this process's environment
// with the account applied.
func SessionEnv(account string) ([]string, error) {
	if account == "" {
		return nil, nil
	}
	return Env(os.Environ(), account)
}

// List returns the names of the accounts that exist on disk, sorted.
func List() ([]string, error) {
	entries, err := os.ReadDir(Root())
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		if e.IsDir() && Validate(e.Name()) == nil {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
