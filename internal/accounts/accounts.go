// Package accounts gives each named account its own Claude Code
// configuration home, so sessions under different accounts stay logged in
// to different Claude accounts side by side.
//
// Claude Code keeps its settings, history and login under CLAUDE_CONFIG_DIR
// (default ~/.claude). On macOS the login token lives in the Keychain, under
// a service name suffixed with a hash of CLAUDE_CONFIG_DIR whenever that
// variable is set — so pointing two sessions at two different directories
// gives them two independent logins. An account is nothing more than such a
// directory, created on first use; there is no registry to keep in sync.
//
// The daemon is the only caller that resolves names to directories, so the
// TUI and the GUI always agree on where an account lives regardless of OS.
package accounts

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/adrg/xdg"
)

// ConfigDirEnv is the variable Claude Code reads its configuration home from.
const ConfigDirEnv = "CLAUDE_CONFIG_DIR"

// secureStorageEnv, when set, overrides which directory Claude Code derives
// its Keychain entry from. An account session must not inherit one from the
// daemon's environment, or every account would share a single login.
const secureStorageEnv = "CLAUDE_SECURESTORAGE_CONFIG_DIR"

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)

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

// Dir returns the configuration home for account name, creating it
// (private to the user) if it doesn't exist yet.
func Dir(name string) (string, error) {
	if err := Validate(name); err != nil {
		return "", err
	}
	dir := filepath.Join(Root(), name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create account %q: %w", name, err)
	}
	return dir, nil
}

// Env returns base with the variables that select account name's
// configuration home, replacing any inherited values of them.
func Env(base []string, name string) ([]string, error) {
	dir, err := Dir(name)
	if err != nil {
		return nil, err
	}
	env := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if strings.HasPrefix(kv, ConfigDirEnv+"=") || strings.HasPrefix(kv, secureStorageEnv+"=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, ConfigDirEnv+"="+dir), nil
}

// SessionEnv is the environment for a session under account: nil (inherit
// the caller's own) for the default account, otherwise this process's
// environment pointed at the account's configuration home.
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
