package config

// AgentProfile is a reusable template for creating a new session: a named
// command (e.g. "claude", "codex", "aider", "shell") plus its default args
// and per-profile overrides.
type AgentProfile struct {
	Name                 string   `yaml:"name"`
	Command              string   `yaml:"command"`
	Args                 []string `yaml:"args,omitempty"`
	IdleAttentionSeconds int      `yaml:"idle_attention_seconds,omitempty"`
	// Account runs the profile's sessions under a named account with its
	// own configuration home and login (see internal/accounts). Empty
	// means the agent's default configuration.
	Account string `yaml:"account,omitempty"`
}

// SessionDef is a persisted session definition (what to relaunch), not live
// process state. Shown as a one-key-relaunchable "stopped" entry on startup.
type SessionDef struct {
	Name    string `yaml:"name"`
	Profile string `yaml:"profile"`
	Cwd     string `yaml:"cwd"`
}
