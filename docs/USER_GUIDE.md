# dispatch user guide

**English** · [Tiếng Việt](USER_GUIDE.vi.md)

dispatch lets you run many AI coding agents (Claude Code, Codex, Aider, or any shell command) at once and keep an eye on all of them from one place — either a terminal UI or a desktop app.

This guide covers installing, day-to-day use of both clients, configuration, updating, and troubleshooting. For the short version, see the [README](../README.md).

- [Core ideas](#core-ideas)
- [Install](#install)
- [The desktop app](#the-desktop-app)
- [The terminal UI](#the-terminal-ui)
- [Multiple accounts](#multiple-accounts)
- [Configuration](#configuration)
- [Updating](#updating)
- [Troubleshooting](#troubleshooting)
- [Uninstalling](#uninstalling)

## Core ideas

**Sessions live in a background daemon, not in your window.** The first time you open dispatch, it starts a small daemon process that owns every agent's terminal. The TUI and the desktop app are just windows onto that daemon. You can close either one, or both, and your agents keep working. Open dispatch again and everything is still there, with its scrollback.

**Both clients share the same sessions.** A session started in the desktop app shows up in the TUI and vice versa, because they talk to the same daemon.

**Each session has a status:**

| Status | Meaning |
|---|---|
| Running | The agent is producing output. |
| Needs attention | The agent has been silent for a while (10 seconds by default, 15 for Claude Code). It is usually waiting for your answer. |
| Exited | The agent's process has ended. Its scrollback is kept until you remove the session. |

"Needs attention" is a timing heuristic: dispatch doesn't read the agent's output, it only notices that it went quiet. An agent that is thinking silently for a long time will also be flagged.

> **Important:** closing a client is always safe. Stopping the *daemon* ends every session it owns. See [Updating](#updating).

## Install

| You want | Get |
|---|---|
| Desktop app on macOS | `dispatch-<version>-mac-arm64.dmg` (Apple Silicon) or `dispatch-<version>-mac-x64.dmg` (Intel) |
| Terminal UI on macOS or Linux | `dispatch_<version>_<os>_<arch>.tar.gz` |

Both are on the [releases page](https://github.com/TuanPhanDuy/dispatch/releases/latest). Windows is not supported.

### Desktop app

1. Open the `.dmg` and drag **dispatch** into **Applications**.
2. The app is not notarized by Apple, so macOS blocks the first launch. Right-click the app → **Open** → **Open**, or run:

   ```sh
   xattr -dr com.apple.quarantine /Applications/dispatch.app
   ```

The app contains its own copy of the `dispatch` binary and starts the daemon by itself. You don't need to install anything else.

### Terminal UI

```sh
tar -xzf dispatch_*_$(uname -s | tr A-Z a-z)_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz
sudo mv dispatch /usr/local/bin/
dispatch --version
```

On macOS, if the binary is blocked: `xattr -d com.apple.quarantine /usr/local/bin/dispatch`.

With Go installed you can instead run `go install github.com/TuanPhanDuy/dispatch/cmd/dispatch@latest`.

### Agents

dispatch launches agent CLIs; it doesn't install them. Install the ones you want (for example Claude Code or Codex) so that they work when you type their name in a terminal. The desktop app reads your login shell's `PATH`, so anything your terminal can find, the app can find too.

## The desktop app

### Session list

This is the first screen. Each row shows a session's name, status, agent command, and working directory.

- **+ New session** (`⌘N`): pick a name, an agent, and a working directory.
- **Attach** opens the session in a terminal pane. **Restart**, **Kill**, **Rename**, and **Remove** (for exited sessions) are on the same row.
- **Flat / By project** groups sessions by working directory.
- The strip above the list holds **recent launches**. Click one to start the same agent in the same directory again.
- **Jump to session** (`⌘K`) searches sessions and recent launches by name or path.

When a session goes from running to *needs attention*, you get a desktop notification and an in-app toast; clicking either opens that session. The bell icon and the Dock badge show how many sessions are waiting on you.

### Tabs and panes

Every session you attach becomes a **tab**. Tabs stay connected in the background, so switching between them is instant and never reloads scrollback.

The area under the tabs can be split into **panes**, so you can watch several agents side by side:

1. Press `⌘D` to split the focused pane to the right, or `⌘⇧D` to split it downward. You can also use the split buttons in the top bar or on each pane's header.
2. Choose which session goes into the new pane: an existing one, a recent launch, or **New session…**.
3. Drag the line between two panes to resize them. Each agent's terminal is resized to fit its pane, so full-screen agents redraw correctly.

Working with panes:

| Do this | How |
|---|---|
| Focus another pane | Click it, or `⌘⌥` + arrow keys |
| Make one pane fill the window, and back | `⌘⇧↵`, or the expand icon on its header |
| Put a background tab into the focused pane | Click the tab |
| Open a background tab next to the focused pane | The split icon on that tab |
| Close a pane but keep the session as a tab | `⌘⇧W`, or × on the pane header |
| Detach a tab completely | `⌘W`, or × on the tab |

The focused pane has a coloured border. A pane whose agent needs attention, and that isn't focused, pulses orange until you look at it.

Your pane layout is saved. The next time you open the app, the same sessions reopen in the same arrangement; sessions that have exited in the meantime are skipped.

### Keyboard shortcuts

| Shortcut | Action |
|---|---|
| `⌘K` | Jump to a session |
| `⌘N` | New session |
| `⌘1` – `⌘9` | Go to tab 1–9 |
| `⌘]` / `⌘[` | Next / previous tab |
| `⌘D` / `⌘⇧D` | Split right / split down |
| `⌘⌥` + arrows | Move between panes |
| `⌘⇧↵` | Zoom / unzoom pane |
| `⌘⇧W` | Close pane (keep the tab) |
| `⌘W` | Detach the current tab |
| `⌘/` | Show the shortcut list |

Everything else you type goes straight to the agent in the focused pane, including `ctrl+C` and `ctrl+D`.

## The terminal UI

Run:

```sh
dispatch
```

### Session list

| Key | Action |
|---|---|
| `↑` / `↓`, `j` / `k` | Move the selection |
| `enter` | Attach to a running session, or launch a saved one |
| `n` | New session |
| `r` | Rename |
| `R` | Restart |
| `d` | Kill a running session, or remove an exited/saved one (asks first) |
| `?` | Help |
| `q`, `ctrl+C` | Quit the TUI — sessions keep running |

**New session** is a short wizard: choose a profile (see [Configuration](#configuration)), type a name, confirm the working directory. `enter` moves to the next step and `esc` cancels.

### Inside a session

Everything you type goes to the agent. The only reserved key is `ctrl+\`, which opens a one-shot command menu:

| `ctrl+\` then | Action |
|---|---|
| `d` | Detach and return to the list (session keeps running) |
| `k` | Kill the session |
| `q` | Quit the TUI |
| `esc` | Cancel |

The TUI shows one session at a time. Use the desktop app if you want several side by side.

## Multiple accounts

Sessions can run under named **accounts**, so the same tool can be signed in to different accounts side by side — say Claude Code on your personal plan in one pane, Claude Code on your company's plan in the next, and Codex on a third account below them.

An account is a separate identity for your agent CLIs. For each supported tool it holds that tool's own login, settings, history and other user-level files — everything that normally lives in `~/.claude` or `~/.codex`. Project files, such as a repository's `.claude/` folder or `AGENTS.md`, are still shared, because they belong to the project. Sessions that don't pick an account keep using your usual logins.

> Use accounts to keep genuinely separate identities apart, such as personal and work. Spreading one person's work over several accounts to get around a plan's usage limits may break the tool provider's terms; check them for your situation.

### Supported tools

| Tool | Kept per account | How |
|---|---|---|
| Claude Code | login, settings, history, MCP servers, user `CLAUDE.md` | `CLAUDE_CONFIG_DIR` → `<account>/claude` |
| Codex CLI | login (`auth.json` or keychain), `config.toml`, history | `CODEX_HOME` → `<account>/codex` |
| Anything else | whatever you put in the account's `env` file | see [Other tools](#other-tools) |

Both variables are set for **every** session under the account, whatever agent it starts. So a **Shell** session under `work` also runs `claude` and `codex` as `work` — handy for tools you launch by hand.

### Signing in to several accounts

Each account needs to be signed in once per tool. After that, every session you open under it is already signed in, so you can run as many sessions per account as you like.

1. **Create the first account's session.** Click **+ New session**, choose the agent (for example Claude Code), set **Account** to **+ New account…**, type a name such as `personal`, and click **Create session**.
2. **Sign in inside that session.**
   - *Claude Code* starts like a fresh install: pick a theme, then choose how to log in. It opens your browser; sign in with the account you want for `personal`. To sign in again later, type `/login`.
   - *Codex* asks you to sign in the first time it runs: choose **Sign in with ChatGPT** (opens the browser) or paste an API key. You can also run `codex login` from a **Shell** session under the account.
3. **Repeat for the next account**, e.g. a new account `work`, signing in with your work identity.
4. **Open more sessions.** Pick **@personal** or **@work** in the **Account** field and they start already signed in. Restarting a session keeps its account, and recent launches relaunch under the same one.

**Browser tip:** the browser sign-in uses whatever Claude or ChatGPT account the browser is currently logged in to. Before signing in an account, make sure the browser is on the right identity — the easiest way is a separate browser profile per account (copy the sign-in link into it), or signing out of claude.ai / chatgpt.com first.

To check which account a session is signed in as, use the tool's own status command, e.g. `/status` in Claude Code or `/status` in Codex.

### Seeing which account a session uses

Sessions under an account carry an **@name** chip in the session list, on their tab, on their pane header and in the ⌘K palette. Sessions without a chip use your usual logins.

### In the terminal UI

Add a profile with an `account` to your [config.yaml](#configuration) for each combination you want:

```yaml
profiles:
  - name: claude-work
    command: claude
    account: work
  - name: claude-personal
    command: claude
    account: personal
  - name: codex-work
    command: codex
    account: work
```

Pick the profile in the **New session** wizard. The session list shows the account next to each session's status. The TUI and the desktop app share accounts: `account: work` is the same as **@work** in the desktop app, with the same logins.

### Other tools

Each account has an `env` file (created with commented examples) whose `KEY=VALUE` lines are added to every session under that account. Use it for:

- **API keys**, for tools that sign in with one, such as Aider:

  ```sh
  OPENAI_API_KEY=sk-...
  ANTHROPIC_API_KEY=sk-ant-...
  ```

- **Another tool's configuration home**, if it has one. For example Gemini CLI reads `GEMINI_CLI_HOME`:

  ```sh
  GEMINI_CLI_HOME=/Users/you/Library/Application Support/dispatch/accounts/work/gemini
  ```

  Whether that also separates the tool's *login* depends on the tool — if it keeps credentials in a shared place such as the system keychain, accounts may still share one. Check the tool's documentation.

Lines starting with `#` are ignored, `export ` and surrounding quotes are allowed. Changes apply to sessions started or restarted afterwards. A malformed line stops the session from starting and the error names the line.

### Where accounts live

| OS | Folder |
|---|---|
| macOS | `~/Library/Application Support/dispatch/accounts/<name>` |
| Linux | `~/.local/share/dispatch/accounts/<name>` (or `$XDG_DATA_HOME/dispatch/accounts/<name>`) |

Inside: `claude/`, `codex/` and `env`. The folder is private to your user; the `env` file may hold secrets, so don't share it.

### Removing an account

1. Sign out of each tool in a session under that account — `/logout` in Claude Code, `codex logout` in a Shell session — so any keychain entries go too.
2. Delete its folder, e.g. `rm -rf ~/Library/Application\ Support/dispatch/accounts/work`.

## Configuration

The terminal UI reads `config.yaml`, creating it with defaults on first run:

- macOS: `~/Library/Application Support/dispatch/config.yaml`
- Linux: `~/.config/dispatch/config.yaml` (or `$XDG_CONFIG_HOME/dispatch/config.yaml`)

```yaml
defaults:
  scrollback_lines: 5000        # lines of history kept per session
  idle_attention_seconds: 10    # silence before "needs attention"

profiles:                       # what the TUI's New session wizard offers
  - name: claude
    command: claude
    idle_attention_seconds: 15  # per-profile override
  - name: codex
    command: codex
    args: ["--full-auto"]
  - name: shell
    command: zsh
  - name: claude-work
    command: claude
    account: work               # run under a separate account

sessions: []                    # written automatically; see below
```

- **profiles** are templates. Add one for any CLI you like, with its arguments. `account` runs its sessions under a [separate account](#multiple-accounts).
- **sessions** is maintained by dispatch: each session you create or rename is saved with its name, profile, and directory, so it stays in the list after it exits and can be relaunched with `enter`.

The desktop app doesn't use profiles. Its **New session** dialog offers Claude Code, Codex, and Aider when they're installed, plus your login shell, and it keeps its own list of recent launches.

Scrollback size and the default idle time are fixed when the daemon starts, by whichever client starts it first. Changes take effect after the daemon is restarted.

## Updating

1. Install the new version the same way you installed the old one.
2. Restart the daemon so it runs the new code. **This ends every running session**, so finish or save your agents' work first:

   ```sh
   pkill -f "dispatch daemon"
   ```

3. Open dispatch again; it starts a fresh daemon.

Until you restart it, the old daemon keeps running the old version, even if the clients are new.

## Troubleshooting

**"dispatch can't be opened because Apple cannot check it for malicious software."**
The app isn't notarized. Right-click → **Open**, or run `xattr -dr com.apple.quarantine /Applications/dispatch.app`.

**An agent isn't offered in the desktop app's New session dialog.**
The app only lists agents it can find on your login shell's `PATH`. Check that `which claude` (or `codex`, `aider`) works in a new terminal window, then restart the app.

**A session exits immediately with "command not found".**
The daemon couldn't find the command. If the daemon was started from somewhere with a different `PATH`, restart it from your normal environment (see [Updating](#updating)).

**"Could not connect to daemon".**
dispatch talks to the daemon over a socket at `$XDG_RUNTIME_DIR/dispatch.sock` if that variable is set, otherwise `$TMPDIR/dispatch-<your uid>/dispatch.sock`. If a crashed daemon left a stale socket behind, stop any leftover daemon with `pkill -f "dispatch daemon"` and open dispatch again.

**In the desktop app, only one of several open tabs updates.**
Your daemon predates v0.2.0, which could only stream one session per window. Restart it (see [Updating](#updating)).

**The Account field is greyed out, saying to restart the daemon.**
Your daemon predates accounts (v0.4.0). Restart it (see [Updating](#updating)). Until then, sessions run under your default login.

**A session under an account asks me to sign in every time.**
That account hasn't been signed in to that tool yet: sign in once (see [Signing in to several accounts](#signing-in-to-several-accounts)) and later sessions under it reuse the login. If it keeps asking, check that the account's folder still exists (see [Where accounts live](#where-accounts-live)).

**A session under an account fails to start with "env line N: expected KEY=VALUE".**
Fix that line in the account's `env` file (see [Other tools](#other-tools)), then start or restart the session.

**"Needs attention" fires while the agent is still working.**
Some agents think silently for long stretches. Raise `idle_attention_seconds` for that profile.

**All my sessions disappeared.**
The daemon was stopped: by a restart, a crash, logging out, or rebooting. Sessions don't survive the daemon. In the TUI, saved sessions stay in the list and can be relaunched with `enter`; in the desktop app, use the recent launches strip.

## Uninstalling

```sh
pkill -f "dispatch daemon"                          # ends all sessions
rm -rf /Applications/dispatch.app                   # desktop app
sudo rm -f /usr/local/bin/dispatch                  # terminal UI
rm -rf ~/Library/Application\ Support/dispatch      # macOS settings, recents, layout, accounts
rm -rf ~/.config/dispatch ~/.local/share/dispatch   # Linux settings and accounts
```

If you used [accounts](#multiple-accounts), sign out of each tool under each account first (see [Removing an account](#removing-an-account)), so their keychain entries go too.
