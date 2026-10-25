package main

import (
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/tuanbin007/dispatch/internal/client"
	"github.com/tuanbin007/dispatch/internal/config"
	"github.com/tuanbin007/dispatch/internal/daemon"
	"github.com/tuanbin007/dispatch/internal/ptyproc"
	"github.com/tuanbin007/dispatch/internal/tui"
)

// version/commit/date are injected by goreleaser via -ldflags at release
// build time; they stay "dev"/"none"/"unknown" for local `go build`/`go run`.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	root := &cobra.Command{
		Use:     "dispatch",
		Short:   "Manage multiple AI coding-agent CLI sessions from one terminal UI",
		Version: fmt.Sprintf("%s (commit %s, built %s)", version, commit, date),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	}
	root.AddCommand(debugSpawnCmd())
	root.AddCommand(daemonCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// runTUI is the real entry point: load config, connect to the daemon
// (auto-spawning it on first run), and run the Bubble Tea program. Sessions
// created here keep running after this process exits — the daemon owns
// them, not the TUI.
func runTUI() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	mgr, err := client.Dial(daemon.DefaultSocketPath(), cfg.Defaults.ScrollbackLines, cfg.Defaults.IdleAttentionSeconds)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer mgr.Close()

	app := tui.NewAppModel(mgr, cfg)
	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// daemonCmd is never run directly by users — the client (runTUI, via
// daemon.EnsureRunning) self-execs this, detached, on first connect.
func daemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Run the dispatch daemon (internal; auto-spawned by the client)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			socket, _ := cmd.Flags().GetString("socket")
			scrollback, _ := cmd.Flags().GetInt("scrollback")
			idleSeconds, _ := cmd.Flags().GetInt("idle-seconds")
			return daemon.Run(socket, scrollback, idleSeconds)
		},
	}
	cmd.Flags().String("socket", daemon.DefaultSocketPath(), "unix socket path")
	cmd.Flags().Int("scrollback", 5000, "scrollback lines per session")
	cmd.Flags().Int("idle-seconds", 10, "default idle-attention threshold (seconds)")
	return cmd
}

// debugSpawnCmd proves out raw PTY spawning end-to-end: run a command through
// ptyproc.Process with the controlling terminal in raw mode, full passthrough
// of input/output and window-resize, indistinguishable from running it directly.
func debugSpawnCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "debug-spawn [command] [args...]",
		Short:  "Phase 0 proof: run a command through a PTY with raw passthrough",
		Hidden: true,
		Args:   cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDebugSpawn(args[0], args[1:])
		},
	}
}

func runDebugSpawn(command string, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	cols, rows := 80, 24
	if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		cols, rows = w, h
	}

	proc, err := ptyproc.Start(command, args, cwd, nil, cols, rows)
	if err != nil {
		return fmt.Errorf("spawn %s: %w", command, err)
	}
	defer proc.Close()

	stopResize := ptyproc.WatchResize(func() {
		if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
			_ = proc.Resize(w, h)
		}
	})
	defer stopResize()

	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		oldState, err := term.MakeRaw(fd)
		if err != nil {
			return fmt.Errorf("set raw mode: %w", err)
		}
		defer term.Restore(fd, oldState)
	}

	go func() { _, _ = io.Copy(proc, os.Stdin) }()
	go func() { _, _ = io.Copy(os.Stdout, proc) }()

	return proc.Wait()
}
