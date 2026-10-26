package accounts

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestEnvPointsAtAPrivatePerAccountDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv(RootEnv, root)

	base := []string{"PATH=/usr/bin", "CLAUDE_CONFIG_DIR=/elsewhere", "CLAUDE_SECURESTORAGE_CONFIG_DIR=/shared", "HOME=/home/u"}
	env, err := Env(base, "work")
	if err != nil {
		t.Fatalf("Env: %v", err)
	}
	want := []string{"PATH=/usr/bin", "HOME=/home/u", "CLAUDE_CONFIG_DIR=" + filepath.Join(root, "work")}
	if !reflect.DeepEqual(env, want) {
		t.Fatalf("Env = %q, want %q", env, want)
	}

	info, err := os.Stat(filepath.Join(root, "work"))
	if err != nil {
		t.Fatalf("account dir not created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("account dir perm = %o, want 700", perm)
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
