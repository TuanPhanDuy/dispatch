# dispatch

A friendly terminal UI for managing many AI coding-agent CLI sessions (Claude Code, Codex, Aider, or any shell command) at once — think tmux, but purpose-built for juggling AI agents instead of raw shells.

Sessions are owned by a small background daemon, not the terminal window you opened them from. Close the TUI, close the terminal, come back tomorrow — `dispatch` reattaches to everything that's still running.

## Install

### Download a prebuilt binary (macOS/Linux)

Grab the archive for your platform from the [latest release](https://github.com/TuanPhanDuy/dispatch/releases/latest), then:

```sh
tar -xzf dispatch_*_$(uname -s | tr A-Z a-z)_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz
sudo mv dispatch /usr/local/bin/
```

On macOS, if Gatekeeper blocks the unsigned binary: `xattr -d com.apple.quarantine /usr/local/bin/dispatch`.

### Desktop app (macOS)

Download `dispatch-<version>-mac-arm64.dmg` (Apple Silicon) or `-mac-x64.dmg` (Intel) from the [latest release](https://github.com/TuanPhanDuy/dispatch/releases/latest) and drag **dispatch** into Applications. The app bundles its own `dispatch` binary and starts the daemon itself.

The app isn't notarized by Apple, so the first launch is blocked. Either right-click the app → **Open** → **Open**, or run:

```sh
xattr -dr com.apple.quarantine /Applications/dispatch.app
```

### Go

```sh
go install github.com/TuanPhanDuy/dispatch/cmd/dispatch@latest
```

### From source

```sh
git clone https://github.com/TuanPhanDuy/dispatch
cd dispatch
make build
./dispatch
```

## Usage

Just run:

```sh
dispatch
```

The first run auto-starts a background daemon (you never run this yourself) and opens the session list.

### Keymap

**List view** (not attached to anything):

| Key | Action |
|---|---|
| `↑`/`↓`, `j`/`k` | move selection |
| `enter` | attach to a running session, or launch a stopped one |
| `n` | new session |
| `r` | rename |
| `d` | kill (running) / remove (exited or stopped) — asks to confirm |
| `R` | restart a live session |
| `?` | help overlay |
| `q`, `ctrl+c` | quit the TUI — sessions keep running |

**Attached view** (inside a session):

Everything you type is forwarded straight to the agent — arrow keys, `ctrl+c`, all of it. The one reserved key is `ctrl+\`, which opens a one-shot command mode:

| After `ctrl+\` | Action |
|---|---|
| `d` | detach (session keeps running) |
| `k` | kill |
| `q` | quit the TUI |

### Desktop app (tiled panes)

The desktop app (see Install, or `cd gui && npm install && npm start` from source) is an Electron client for the same daemon. It tiles sessions side by side so you can watch several agents at once:

| Key | Action |
|---|---|
| `⌘D` / `⌘⇧D` | split the focused pane right / down (pick or launch a session) |
| `⌘⌥` + arrows | move focus between panes |
| `⌘⇧↵` | zoom / unzoom the focused pane |
| `⌘⇧W` | close the pane (the session stays open as a tab) |

Drag a divider to resize. Each pane's PTY is resized to fit it, and panes whose agent has gone idle pulse until you look at them. The layout is restored the next time you open the app.

## Configuration

`~/.config/dispatch/config.yaml` (or `~/Library/Application Support/dispatch/config.yaml` on macOS):

```yaml
defaults:
  scrollback_lines: 5000
  idle_attention_seconds: 10

profiles:
  - name: claude
    command: claude
    idle_attention_seconds: 15
  - name: codex
    command: codex
    args: ["--full-auto"]
  - name: shell
    command: zsh
```

`profiles` are the templates the "new session" wizard picks from. `sessions` (written automatically as you create/rename sessions) is a list of saved name/profile/working-directory definitions — closing a session doesn't delete its entry, so you can relaunch it later with one keypress.

## Architecture

- **Daemon/client split**: a background daemon (auto-spawned, auto-detached) owns every session's PTY, scrollback, and idle-attention tracking. The TUI is a thin client talking to it over a local unix socket. This is what makes sessions survive closing the terminal.
- **"Needs attention" detection**: a session is flagged when its output has gone quiet for a while (configurable per profile) — a lightweight heuristic, not prompt-content parsing.
- Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lipgloss](https://github.com/charmbracelet/lipgloss), and [creack/pty](https://github.com/creack/pty).

## Known limitations (v1)

- **No Windows support yet.** Sessions are spawned through Unix PTYs (`creack/pty` + `syscall.SysProcAttr`), which has no Windows equivalent without a separate ConPTY-based implementation. Planned for a future release, not v1.
- If the daemon itself is killed (not just the TUI), its sessions die with it — only closing the *TUI* is safe.
- The idle heuristic is output-silence timing only; it doesn't try to detect "waiting for your input" vs. "thinking."

## License

MIT — see [LICENSE](LICENSE).
