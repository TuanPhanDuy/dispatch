package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/TuanPhanDuy/dispatch/internal/accounts"
	"github.com/TuanPhanDuy/dispatch/internal/config"
	"github.com/TuanPhanDuy/dispatch/internal/protocol"
)

// printConfigDir is a profile whose process prints the tool homes and the
// EXTRA variable it was started with, then stays alive.
func printConfigDir(account string) config.AgentProfile {
	return config.AgentProfile{
		Name:    "probe",
		Command: "sh",
		Args:    []string{"-c", `echo "CFG=[$CLAUDE_CONFIG_DIR] CODEX=[$CODEX_HOME] EXTRA=[$EXTRA]"; exec cat`},
		Account: account,
	}
}

func probeLine(claude, codex, extra string) string {
	return "CFG=[" + claude + "] CODEX=[" + codex + "] EXTRA=[" + extra + "]"
}

func scrollbackOf(r *Registry, id string) string {
	r.mu.Lock()
	s := r.sessions[id]
	r.mu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ring.Snapshot()
}

func TestSessionsRunUnderTheirOwnAccountConfigDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv(accounts.RootEnv, root)
	t.Setenv("CLAUDE_CONFIG_DIR", "/inherited/from/daemon")
	t.Setenv("CODEX_HOME", "/inherited/codex")
	t.Setenv("EXTRA", "")

	r := NewRegistry(100, 10)
	defer r.Close()

	work, err := r.Create(printConfigDir("work"), "w", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create work: %v", err)
	}
	personal, err := r.Create(printConfigDir("personal"), "p", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create personal: %v", err)
	}
	def, err := r.Create(printConfigDir(""), "d", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create default: %v", err)
	}

	want := map[string]string{
		work:     probeLine(filepath.Join(root, "work", "claude"), filepath.Join(root, "work", "codex"), ""),
		personal: probeLine(filepath.Join(root, "personal", "claude"), filepath.Join(root, "personal", "codex"), ""),
		def:      probeLine("/inherited/from/daemon", "/inherited/codex", ""), // no account: environment untouched
	}
	for id, line := range want {
		waitUntil(t, 3*time.Second, func() bool { return strings.Contains(scrollbackOf(r, id), line) })
	}

	accountOf := map[string]string{}
	for _, s := range r.List() {
		accountOf[s.ID] = s.Account
	}
	if accountOf[work] != "work" || accountOf[personal] != "personal" || accountOf[def] != "" {
		t.Fatalf("summaries report accounts %v", accountOf)
	}
}

func TestRestartKeepsTheAccountAndPicksUpEnvFileEdits(t *testing.T) {
	root := t.TempDir()
	t.Setenv(accounts.RootEnv, root)
	t.Setenv("EXTRA", "")

	r := NewRegistry(100, 10)
	defer r.Close()

	id, err := r.Create(printConfigDir("work"), "w", ".", 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	claude, codex := filepath.Join(root, "work", "claude"), filepath.Join(root, "work", "codex")
	waitUntil(t, 3*time.Second, func() bool { return strings.Contains(scrollbackOf(r, id), probeLine(claude, codex, "")) })

	if err := os.WriteFile(filepath.Join(root, "work", accounts.EnvFileName), []byte("EXTRA=from-env-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.Restart(id); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	waitUntil(t, 3*time.Second, func() bool {
		return strings.Contains(scrollbackOf(r, id), probeLine(claude, codex, "from-env-file"))
	})
}

func TestCreateRejectsInvalidAccountName(t *testing.T) {
	t.Setenv(accounts.RootEnv, t.TempDir())
	r := NewRegistry(100, 10)
	defer r.Close()

	if _, err := r.Create(printConfigDir("../../etc"), "x", ".", 80, 24); err == nil {
		t.Fatal("Create accepted a path-traversal account name")
	}
	if n := len(r.List()); n != 0 {
		t.Fatalf("a rejected Create still registered %d session(s)", n)
	}
}

func TestServerListsAccountsAndRejectsUnknownRequests(t *testing.T) {
	t.Setenv(accounts.RootEnv, t.TempDir())
	for _, name := range []string{"work", "personal"} {
		if _, err := accounts.Dir(name); err != nil {
			t.Fatalf("Dir: %v", err)
		}
	}
	socketPath, _ := startTestServer(t)
	tc := dialTestClient(t, socketPath)

	tc.send(t, protocol.Envelope{Type: protocol.ReqAccounts, ID: "a1"})
	env := tc.recv(t, protocol.RespAccounts, 2*time.Second)
	var resp protocol.AccountsResp
	if err := protocol.Unmarshal(env.Data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if want := []string{"personal", "work"}; !reflect.DeepEqual(resp.Accounts, want) {
		t.Fatalf("accounts = %v, want %v", resp.Accounts, want)
	}

	tc.send(t, protocol.Envelope{Type: "req.from-the-future", ID: "f1"})
	errEnv := tc.recv(t, protocol.RespError, 2*time.Second)
	if errEnv.ID != "f1" {
		t.Fatalf("error reply has ID %q, want f1", errEnv.ID)
	}
}

func TestServerCreatePassesAccountThrough(t *testing.T) {
	root := t.TempDir()
	t.Setenv(accounts.RootEnv, root)
	socketPath, registry := startTestServer(t)
	tc := dialTestClient(t, socketPath)

	t.Setenv("EXTRA", "")
	p := printConfigDir("work")
	tc.send(t, protocol.Envelope{Type: protocol.ReqCreate, ID: "c1", Data: protocol.Marshal(protocol.CreateSessionReq{
		Name: "w", Command: p.Command, Args: p.Args, Cwd: ".", Cols: 80, Rows: 24, Account: "work",
	})})
	env := tc.recv(t, protocol.RespCreate, 2*time.Second)
	var created protocol.CreateResp
	_ = protocol.Unmarshal(env.Data, &created)

	want := probeLine(filepath.Join(root, "work", "claude"), filepath.Join(root, "work", "codex"), "")
	waitUntil(t, 3*time.Second, func() bool { return strings.Contains(scrollbackOf(registry, created.SessionID), want) })
}
