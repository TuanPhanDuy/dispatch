package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	"gopkg.in/yaml.v3"
)

// Defaults mirrors the `defaults:` YAML block — fallback values applied
// when a profile doesn't override them.
type Defaults struct {
	ScrollbackLines      int `yaml:"scrollback_lines"`
	IdleAttentionSeconds int `yaml:"idle_attention_seconds"`
}

// Daemon mirrors the `daemon:` YAML block.
type Daemon struct {
	IdleTimeoutMinutes int    `yaml:"idle_timeout_minutes"`
	SocketPath         string `yaml:"socket_path"`
}

// Config is the full ~/.config/dispatch/config.yaml document.
type Config struct {
	Daemon   Daemon         `yaml:"daemon"`
	Defaults Defaults       `yaml:"defaults"`
	Profiles []AgentProfile `yaml:"profiles"`
	Sessions []SessionDef   `yaml:"sessions"`

	path string
}

func defaultConfig() *Config {
	return &Config{
		Defaults: Defaults{ScrollbackLines: 5000, IdleAttentionSeconds: 10},
		Profiles: []AgentProfile{
			{Name: "claude", Command: "claude", IdleAttentionSeconds: 15},
			{Name: "codex", Command: "codex", Args: []string{"--full-auto"}},
			{Name: "shell", Command: shellCommand()},
		},
	}
}

func shellCommand() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

// Path returns the config file location, respecting $XDG_CONFIG_HOME.
func Path() (string, error) {
	return xdg.ConfigFile(filepath.Join("dispatch", "config.yaml"))
}

// Load reads the config file, creating it with defaults on first run.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}

	cfg := defaultConfig()
	cfg.path = path

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := cfg.Save(); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.path = path
	cfg.applyDefaults()
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Defaults.ScrollbackLines <= 0 {
		c.Defaults.ScrollbackLines = 5000
	}
	if c.Defaults.IdleAttentionSeconds <= 0 {
		c.Defaults.IdleAttentionSeconds = 10
	}
}

// Save atomically writes the config back (temp file + rename) so a crash
// mid-write can't corrupt it.
func (c *Config) Save() error {
	if c.path == "" {
		return fmt.Errorf("config has no path set (construct it via Load, not Config{})")
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "config-*.yaml.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, c.path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// Profile looks up a named profile.
func (c *Config) Profile(name string) (AgentProfile, bool) {
	for _, p := range c.Profiles {
		if p.Name == name {
			return p, true
		}
	}
	return AgentProfile{}, false
}

// UpsertSession adds or updates a persisted session definition by name.
func (c *Config) UpsertSession(def SessionDef) {
	for i, s := range c.Sessions {
		if s.Name == def.Name {
			c.Sessions[i] = def
			return
		}
	}
	c.Sessions = append(c.Sessions, def)
}

// RemoveSession deletes a persisted session definition by name.
func (c *Config) RemoveSession(name string) {
	out := c.Sessions[:0]
	for _, s := range c.Sessions {
		if s.Name != name {
			out = append(out, s)
		}
	}
	c.Sessions = out
}
