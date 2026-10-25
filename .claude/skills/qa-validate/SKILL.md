---
name: qa-validate
description: >
  Hunts down real bugs in dispatch (the Go daemon/TUI, or the Electron GUI client) by
  actually running the target and driving its real interface — never by reading source and
  predicting behavior — then writes the regression test/testcase that proves each confirmed
  bug and proves the fix. Use after implementing a feature or fix, when asked to "validate",
  "QA this", "find bugs", "test this properly", or before reporting any change as done.
---

# QA validate

Core rule: **a bug is not confirmed, and a fix is not proven, until you've watched the real
thing run and read its actual output.** Reading code and predicting what it does is a
hypothesis, not validation. The Electron GUI's "New session button does nothing" bug in this
repo is the reference case: nothing looked wrong by inspection — `app.js` read fine top to
bottom. Only driving the real button (`webContents.executeJavaScript` clicking the actual
DOM node) and capturing the real renderer console output surfaced the actual failure:
`Uncaught SyntaxError: Identifier 'dispatch' has already been declared` — a collision between
a `const dispatch = window.dispatch` declaration and the non-configurable property
`contextBridge.exposeInMainWorld('dispatch', …)` creates on `window`. No amount of re-reading
`app.js` in isolation would have surfaced that; it only shows up when the real page parses it.

## Workflow

1. **Scope.** Given a bug report, extract exact repro steps. Given a diff/feature, enumerate
   the changed code paths and what each is supposed to do.
2. **Get the real thing running** — pick the path that matches what changed:
   - **Daemon/protocol logic** (`internal/daemon`, `internal/client`, `internal/protocol`):
     run `go test ./... -race` first (this repo's suite already hammers concurrency —
     `TestRestartSwapsProcessWithoutRacingOldPump`, `TestCreateDuplicateNameConcurrentStillUnique`
     — don't re-derive what it already covers). For anything the test suite doesn't reach,
     build (`go build -o dispatch ./cmd/dispatch`), run `./dispatch daemon --socket <path>` in
     the background, and talk to it with a raw newline-JSON socket client (Node's `net` or
     `nc -U`) — the wire format is `internal/protocol/protocol.go`. This needs no TTY, so it's
     fully drivable from Bash.
   - **TUI** (`internal/tui`): its raw-mode PTY/Bubble Tea alt-screen can't be driven headlessly
     from this environment (no interactive stdin passthrough) — favor `internal/client/inprocess`-backed
     unit tests (see `app_test.go`'s pattern of calling `app.Update(someMsg)` directly and
     asserting on `app.list.rows`/`app.mode`) over claiming you "tested the TUI" from reading
     code. If a human needs to eyeball it, say so and ask them to drive it.
   - **Electron GUI** (`gui/`): launch it (`cd gui && npm start`, backgrounded, logs to a file),
     and in `main.js` temporarily wire `mainWindow.webContents.once('did-finish-load', …)` to
     call `webContents.executeJavaScript(...)` that clicks real DOM elements, fills real form
     fields, and reads back real DOM/console state — see the pattern used to catch the bug
     above. Also forward `webContents.on('console-message', …)` to stdout so renderer
     exceptions aren't silently swallowed. Remove the scripted-click block once done; it's a
     diagnostic, not permanent code.
3. **Reproduce, don't assume.** Run the real flow with real input; capture exact observed
   output next to what was expected. A mismatch is a *confirmed* bug. Anything you genuinely
   couldn't execute (no display, missing dependency) is *unverified* — say so; never present
   it as "probably fine."
4. **Pin root cause to file:line** before writing a test. A test for a bug you don't
   understand usually encodes the wrong assertion.
5. **Write one regression test per confirmed bug**, matching the existing style in the
   package you're touching (table-driven Go tests with `waitUntil` helpers for async
   assertions — see `registry_test.go`/`manager_test.go` — or a scripted-click assertion for
   `gui/`). The test must fail against the pre-fix code and pass after; if you can't make it
   fail first, it isn't testing the bug.
6. **Re-run the same real drive from step 2–3** after fixing — a green `go test` run is not
   sufficient proof by itself; confirm the actual symptom is gone the way you found it.
7. **Report**: confirmed bugs (file:line, repro, root cause), tests added (path), anything
   left unverified and why. Don't claim "works" without showing the command output that
   proves it.

## Guardrails

- Anything you create while driving a real run (daemon sessions, processes, temp files) is a
  disposable fixture, not the user's data. Name it identifiably (e.g. a `qa-`-prefixed
  session name) and kill/remove it yourself when done — by the **exact ID you created**, never
  a blanket "remove everything in this state." A prior attempt in this repo to clean up test
  sessions via "kill every non-exited session returned by List()" was correctly blocked
  (auto-mode flagged it as risking the user's real sessions) — the fix was to target the one
  known ID. Apply that same narrowing here.
- If the daemon was already running before you started (check `ps aux | grep "dispatch daemon"`
  and existing sessions via `req.list` first), don't restart/kill it to "get a clean slate" —
  it may be holding the user's real work.

## When you truly cannot run it

No sandbox/display available: fall back to static review — trace each changed path by hand,
enumerate edge cases, write tests for them — but say explicitly "not executed, static review
only" in the report so it isn't mistaken for a live-verified result.
