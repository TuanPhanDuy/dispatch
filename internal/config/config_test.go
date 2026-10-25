package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
)

func withTempXDGHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	xdg.Reload()
	t.Cleanup(xdg.Reload)
	return dir
}

func TestLoadCreatesDefaultConfigOnFirstRun(t *testing.T) {
	home := withTempXDGHome(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Profiles) == 0 {
		t.Fatal("expected default profiles to be populated")
	}
	if cfg.Defaults.ScrollbackLines != 5000 {
		t.Fatalf("ScrollbackLines = %d, want 5000", cfg.Defaults.ScrollbackLines)
	}

	path := filepath.Join(home, "dispatch", "config.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file to be written at %s: %v", path, err)
	}
}

func TestProfileLookup(t *testing.T) {
	withTempXDGHome(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p, ok := cfg.Profile("claude")
	if !ok || p.Command != "claude" {
		t.Fatalf("Profile(\"claude\") = %+v, %v", p, ok)
	}
	if _, ok := cfg.Profile("nonexistent"); ok {
		t.Fatal("expected Profile(\"nonexistent\") to not be found")
	}
}

func TestUpsertAndRemoveSession(t *testing.T) {
	withTempXDGHome(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cfg.UpsertSession(SessionDef{Name: "backend", Profile: "claude", Cwd: "/tmp/backend"})
	if len(cfg.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(cfg.Sessions))
	}

	cfg.UpsertSession(SessionDef{Name: "backend", Profile: "claude", Cwd: "/tmp/backend2"})
	if len(cfg.Sessions) != 1 || cfg.Sessions[0].Cwd != "/tmp/backend2" {
		t.Fatalf("expected upsert to update in place, got %+v", cfg.Sessions)
	}

	cfg.RemoveSession("backend")
	if len(cfg.Sessions) != 0 {
		t.Fatalf("expected session removed, got %+v", cfg.Sessions)
	}
}

func TestSaveAndReload(t *testing.T) {
	withTempXDGHome(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.UpsertSession(SessionDef{Name: "docs", Profile: "codex", Cwd: "/tmp/docs"})
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatalf("Load (reload): %v", err)
	}
	if len(reloaded.Sessions) != 1 || reloaded.Sessions[0].Name != "docs" {
		t.Fatalf("expected persisted session to round-trip, got %+v", reloaded.Sessions)
	}
}
