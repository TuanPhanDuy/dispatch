package accounts

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	for _, name := range []string{"work", "personal", "acme.prod", "a_b-c", "A1"} {
		if err := Validate(name); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range []string{"", ".", "..", ".hidden", "-x", "a/b", "a b", "../escape", "this-name-is-far-too-long-for-an-account-x"} {
		if err := Validate(name); err == nil {
			t.Errorf("Validate(%q) = nil, want an error", name)
		}
	}
}

func TestEnvPointsEveryToolAtItsOwnPrivateHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv(RootEnv, root)

	base := []string{"PATH=/usr/bin", "CLAUDE_CONFIG_DIR=/elsewhere", "CODEX_HOME=/elsewhere/codex", "CLAUDE_SECURESTORAGE_CONFIG_DIR=/shared", "HOME=/home/u"}
	env, err := Env(base, "work")
	if err != nil {
		t.Fatalf("Env: %v", err)
	}
	want := []string{
		"PATH=/usr/bin",
		"HOME=/home/u",
		"CLAUDE_CONFIG_DIR=" + filepath.Join(root, "work", "claude"),
		"CODEX_HOME=" + filepath.Join(root, "work", "codex"),
	}
	if !reflect.DeepEqual(env, want) {
		t.Fatalf("Env = %q, want %q", env, want)
	}

	for _, p := range []string{"work", "work/claude", "work/codex"} {
		info, err := os.Stat(filepath.Join(root, p))
		if err != nil {
			t.Fatalf("%s not created: %v", p, err)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Fatalf("%s perm = %o, want 700", p, perm)
		}
	}
	info, err := os.Stat(filepath.Join(root, "work", EnvFileName))
	if err != nil {
		t.Fatalf("env file template not created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("env file perm = %o, want 600", perm)
	}
}

func TestEnvFileAddsAndOverridesVariables(t *testing.T) {
	root := t.TempDir()
	t.Setenv(RootEnv, root)
	if _, err := Dir("work"); err != nil {
		t.Fatalf("Dir: %v", err)
	}
	file := strings.Join([]string{
		"# keys for aider",
		"",
		"OPENAI_API_KEY=sk-test",
		`export ANTHROPIC_API_KEY="sk-ant test"`,
		"GEMINI_CLI_HOME='/accts/work/gemini'",
		"CODEX_HOME=/custom/codex",
		"PATH=/opt/bin",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, "work", EnvFileName), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}

	env, err := Env([]string{"PATH=/usr/bin", "OPENAI_API_KEY=inherited"}, "work")
	if err != nil {
		t.Fatalf("Env: %v", err)
	}
	want := []string{
		"CLAUDE_CONFIG_DIR=" + filepath.Join(root, "work", "claude"),
		"CODEX_HOME=/custom/codex",
		"OPENAI_API_KEY=sk-test",
		"ANTHROPIC_API_KEY=sk-ant test",
		"GEMINI_CLI_HOME=/accts/work/gemini",
		"PATH=/opt/bin",
	}
	if !reflect.DeepEqual(env, want) {
		t.Fatalf("Env =\n %q\nwant\n %q", env, want)
	}
}

func TestEnvFileSyntaxErrorNamesTheLine(t *testing.T) {
	root := t.TempDir()
	t.Setenv(RootEnv, root)
	if _, err := Dir("work"); err != nil {
		t.Fatalf("Dir: %v", err)
	}
	_ = os.WriteFile(filepath.Join(root, "work", EnvFileName), []byte("# ok\nGOOD=1\nnot a pair\n"), 0o600)

	_, err := Env(nil, "work")
	if err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("Env error = %v, want one naming line 3", err)
	}
}

func TestDirKeepsAnExistingEnvFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv(RootEnv, root)
	if err := os.MkdirAll(filepath.Join(root, "work"), 0o700); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(root, "work", EnvFileName)
	_ = os.WriteFile(envFile, []byte("MINE=1\n"), 0o600)

	if _, err := Dir("work"); err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if got, _ := os.ReadFile(envFile); string(got) != "MINE=1\n" {
		t.Fatalf("Dir overwrote the env file: %q", got)
	}
}

func TestEnvRejectsInvalidName(t *testing.T) {
	t.Setenv(RootEnv, t.TempDir())
	if _, err := Env(nil, "../escape"); err == nil {
		t.Fatal("Env accepted a path-traversal account name")
	}
}

func TestListReturnsExistingAccountsSorted(t *testing.T) {
	root := t.TempDir()
	t.Setenv(RootEnv, root)

	if got, err := List(); err != nil || len(got) != 0 {
		t.Fatalf("List on empty root = %v, %v; want empty", got, err)
	}
	for _, name := range []string{"work", "personal"} {
		if _, err := Dir(name); err != nil {
			t.Fatalf("Dir(%s): %v", name, err)
		}
	}
	_ = os.WriteFile(filepath.Join(root, "stray-file"), nil, 0o600)
	_ = os.Mkdir(filepath.Join(root, ".hidden"), 0o700)

	got, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := []string{"personal", "work"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("List = %v, want %v", got, want)
	}
}

func TestListWithMissingRoot(t *testing.T) {
	t.Setenv(RootEnv, filepath.Join(t.TempDir(), "does-not-exist"))
	got, err := List()
	if err != nil || len(got) != 0 {
		t.Fatalf("List = %v, %v; want empty, nil", got, err)
	}
}
