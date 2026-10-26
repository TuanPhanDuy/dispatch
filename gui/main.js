// Electron main process: owns the one socket connection to the dispatch
// daemon (same unix-socket wire protocol the Go TUI speaks) and exposes it
// to the renderer over IPC. The daemon itself is unmodified — this is just
// a second client sitting next to internal/client/client.go, written in
// JS because it lives in its own Electron app instead of the Go binary.
const { app, BrowserWindow, ipcMain, dialog } = require('electron');
const net = require('net');
const os = require('os');
const path = require('path');
const fs = require('fs');
const crypto = require('crypto');
const { spawn, execFileSync } = require('child_process');

const CALL_TIMEOUT_MS = 10000;

const MsgType = {
  ReqList: 'req.list',
  ReqCreate: 'req.create',
  ReqAttach: 'req.attach',
  ReqDetach: 'req.detach',
  ReqInput: 'req.input',
  ReqResize: 'req.resize',
  ReqKill: 'req.kill',
  ReqRestart: 'req.restart',
  ReqRename: 'req.rename',
  ReqRemove: 'req.remove',
  ReqAccounts: 'req.accounts',
  RespOK: 'resp.ok',
  RespError: 'resp.error',
  RespList: 'resp.list',
  RespCreate: 'resp.create',
  RespAttach: 'resp.attach',
  EventOutput: 'event.output',
  EventSessionList: 'event.session_list',
};

// Mirrors daemon.DefaultSocketPath() in internal/daemon/spawn.go — both the
// Go TUI and this GUI must resolve to the same path to share one daemon.
function defaultSocketPath() {
  const dir = process.env.XDG_RUNTIME_DIR || path.join(os.tmpdir(), `dispatch-${process.getuid()}`);
  return path.join(dir, 'dispatch.sock');
}

// Finds the `dispatch` Go binary so this GUI can self-exec a daemon the
// same way the TUI does (daemon.EnsureRunning). Checked in order: an
// explicit override, the copy bundled into the packaged app, the sibling
// binary one level up from this file (the repo-root build during
// development), then whatever's on PATH.
function resolveDispatchBinary() {
  if (process.env.DISPATCH_BIN) return process.env.DISPATCH_BIN;
  if (app.isPackaged) return path.join(process.resourcesPath, 'bin', 'dispatch');
  const sibling = path.join(__dirname, '..', 'dispatch');
  if (fs.existsSync(sibling)) return sibling;
  return 'dispatch';
}

// Candidate AI coding-agent CLIs, mirroring the TUI's default config
// profiles (internal/config/config.go's defaultConfig) — same commands/args,
// so a session launched from either client behaves identically. Only the
// ones actually found on PATH are offered; "Shell" is always offered since
// it's always available, same as the TUI's always-present shell profile.
const AGENT_CANDIDATES = [
  { id: 'claude', label: 'Claude Code', command: 'claude', args: [] },
  { id: 'codex', label: 'Codex', command: 'codex', args: ['--full-auto'] },
  { id: 'aider', label: 'Aider', command: 'aider', args: [] },
];

// An app opened from Finder/Dock inherits launchd's bare PATH
// (/usr/bin:/bin:/usr/sbin:/sbin), so agent CLIs installed via npm,
// Homebrew, etc. would be invisible to detectAgents — and to the daemon
// this app spawns, which runs every session with its environment. Adopt
// the user's login-shell PATH instead, the same one their terminal has.
function adoptLoginShellPath() {
  if (process.platform !== 'darwin' || !app.isPackaged) return;
  const shell = process.env.SHELL || '/bin/zsh';
  try {
    const out = execFileSync(shell, ['-ilc', 'printf "__PATH__%s__PATH__" "$PATH"'], { encoding: 'utf8', timeout: 5000 });
    const m = out.match(/__PATH__(.*)__PATH__/);
    if (m && m[1]) process.env.PATH = m[1];
  } catch { /* keep launchd's PATH — the Shell agent still works */ }
}

function isOnPath(cmd) {
  const dirs = (process.env.PATH || '').split(path.delimiter);
  return dirs.some((dir) => {
    try {
      fs.accessSync(path.join(dir, cmd), fs.constants.X_OK);
      return true;
    } catch {
      return false;
    }
  });
}

function detectAgents() {
  const detected = AGENT_CANDIDATES.filter((c) => isOnPath(c.command));
  const shell = process.env.SHELL || '/bin/sh';
  detected.push({ id: 'shell', label: `Shell (${path.basename(shell)})`, command: shell, args: [] });
  return detected;
}

// Recents is this GUI's own lightweight quick-launch list (name/command/args/cwd),
// separate from the TUI's config.yaml `sessions:` — both ultimately just launch
// through the same daemon, but unifying the two persistence stores isn't needed
// for this to be useful, and risks corrupting the TUI's config if the mapping
// between a GUI "agent" and a TUI "profile" name ever drifts.
const RECENTS_MAX = 8;

function recentsPath() {
  return path.join(app.getPath('userData'), 'recents.json');
}

function loadRecents() {
  try {
    return JSON.parse(fs.readFileSync(recentsPath(), 'utf8'));
  } catch {
    return [];
  }
}

function saveRecents(list) {
  fs.mkdirSync(path.dirname(recentsPath()), { recursive: true });
  fs.writeFileSync(recentsPath(), JSON.stringify(list, null, 2));
}

function addRecent(entry) {
  const list = loadRecents().filter((r) => r.name !== entry.name);
  list.unshift(entry);
  const trimmed = list.slice(0, RECENTS_MAX);
  saveRecents(trimmed);
  return trimmed;
}

function dialOK(socketPath) {
  return new Promise((resolve) => {
    const sock = net.createConnection({ path: socketPath });
    const timer = setTimeout(() => {
      sock.destroy();
      resolve(false);
    }, 200);
    sock.once('connect', () => {
      clearTimeout(timer);
      sock.end();
      resolve(true);
    });
    sock.once('error', () => {
      clearTimeout(timer);
      resolve(false);
    });
  });
}

// Mirrors daemon.EnsureRunning in internal/daemon/spawn.go: if nothing is
// listening on socketPath, self-exec `dispatch daemon --socket <path>`
// detached, then poll until it comes up.
async function ensureDaemonRunning(socketPath, scrollbackCap, defaultIdleSec) {
  if (await dialOK(socketPath)) return;

  fs.mkdirSync(path.dirname(socketPath), { recursive: true, mode: 0o700 });
  const bin = resolveDispatchBinary();
  const child = spawn(
    bin,
    ['daemon', '--socket', socketPath, '--scrollback', String(scrollbackCap), '--idle-seconds', String(defaultIdleSec)],
    { detached: true, stdio: 'ignore' }
  );
  child.on('error', (err) => {
    throw new Error(`spawn daemon (${bin}): ${err.message}`);
  });
  child.unref();

  const deadline = Date.now() + 3000;
  while (Date.now() < deadline) {
    if (await dialOK(socketPath)) return;
    await new Promise((r) => setTimeout(r, 25));
  }
  throw new Error(`daemon did not come up on ${socketPath} within 3s`);
}

function toU8(buf) {
  return buf && buf.length ? new Uint8Array(buf) : new Uint8Array(0);
}

// SocketClient speaks the same newline-delimited-JSON protocol as
// internal/client/client.go's SocketManager. []byte wire fields are
// base64 strings (Go's encoding/json convention for []byte) and are
// decoded to Buffers here.
class SocketClient {
  constructor(socketPath) {
    this.socketPath = socketPath;
    this.sock = null;
    this.buf = '';
    this.pending = new Map(); // id -> {resolve, reject, timer}
    this.outputSubs = new Map(); // sessionId -> (ev) => void
    this.listSubs = new Set(); // (sessions) => void
  }

  connect() {
    return new Promise((resolve, reject) => {
      const sock = net.createConnection({ path: this.socketPath }, () => resolve());
      sock.once('error', reject);
      sock.setEncoding('utf8');
      sock.on('data', (chunk) => this._onData(chunk));
      sock.on('close', () => this._onClose());
      this.sock = sock;
    });
  }

  _onData(chunk) {
    this.buf += chunk;
    let idx;
    while ((idx = this.buf.indexOf('\n')) >= 0) {
      const line = this.buf.slice(0, idx);
      this.buf = this.buf.slice(idx + 1);
      if (!line) continue;
      let env;
      try {
        env = JSON.parse(line);
      } catch {
        continue;
      }
      this._handleEnvelope(env);
    }
  }

  _handleEnvelope(env) {
    if (env.type === MsgType.EventSessionList) {
      const sessions = (env.data && env.data.sessions) || [];
      for (const fn of this.listSubs) fn(sessions);
      return;
    }
    if (env.type === MsgType.EventOutput) {
      const d = env.data || {};
      const sub = this.outputSubs.get(d.session_id);
      const bytes = d.bytes ? Buffer.from(d.bytes, 'base64') : null;
      if (sub) sub({ bytes, exited: !!d.exited });
      if (d.exited) this.outputSubs.delete(d.session_id);
      return;
    }

    const pending = this.pending.get(env.id);
    if (!pending) return;
    this.pending.delete(env.id);
    clearTimeout(pending.timer);
    if (env.type === MsgType.RespError) {
      const message = (env.data && env.data.message) || 'daemon error';
      pending.reject(new Error(message));
    } else {
      pending.resolve(env);
    }
  }

  // Closing the connection must unblock every caller still waiting on it,
  // the same way SocketManager.handleDisconnect does in the Go client.
  _onClose() {
    for (const [, p] of this.pending) {
      clearTimeout(p.timer);
      p.reject(new Error('daemon connection closed'));
    }
    this.pending.clear();
    for (const [, fn] of this.outputSubs) fn({ bytes: null, exited: true });
    this.outputSubs.clear();
    this.listSubs.clear();
  }

  _writeEnvelope(env) {
    this.sock.write(JSON.stringify(env) + '\n');
  }

  call(type, data, timeoutMs = CALL_TIMEOUT_MS) {
    return new Promise((resolve, reject) => {
      const id = crypto.randomUUID();
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error('timed out waiting for daemon response'));
      }, timeoutMs);
      this.pending.set(id, { resolve, reject, timer });
      this._writeEnvelope({ type, id, data });
    });
  }

  async list() {
    const resp = await this.call(MsgType.ReqList, null);
    return (resp.data && resp.data.sessions) || [];
  }

  async create({ name, command, args, cwd, cols, rows, account }) {
    const resp = await this.call(MsgType.ReqCreate, { name, command, args: args || [], cwd, cols, rows, account: account || undefined });
    return resp.data.session_id;
  }

  // Resolves null (not an empty list) when the daemon predates accounts:
  // such a daemon would silently ignore `account` on create and run the
  // session under the default login, so the UI must not offer the choice.
  async accounts() {
    try {
      const resp = await this.call(MsgType.ReqAccounts, null, 2000);
      return (resp.data && resp.data.accounts) || [];
    } catch {
      return null;
    }
  }

  async attach(sessionId, cols, rows, onOutput) {
    this.outputSubs.set(sessionId, onOutput);
    try {
      const resp = await this.call(MsgType.ReqAttach, { session_id: sessionId, cols, rows });
      return resp.data.scrollback ? Buffer.from(resp.data.scrollback, 'base64') : Buffer.alloc(0);
    } catch (err) {
      this.outputSubs.delete(sessionId);
      throw err;
    }
  }

  async detach(sessionId) {
    this.outputSubs.delete(sessionId);
    await this.call(MsgType.ReqDetach, { session_id: sessionId });
  }

  // Fire-and-forget, like SocketManager.Write/Resize — the daemon never
  // acks these, so waiting for a round trip per keystroke would add
  // latency for no benefit.
  write(sessionId, text) {
    this._writeEnvelope({
      type: MsgType.ReqInput,
      data: { session_id: sessionId, bytes: Buffer.from(text, 'utf8').toString('base64') },
    });
  }

  resize(sessionId, cols, rows) {
    this._writeEnvelope({ type: MsgType.ReqResize, data: { session_id: sessionId, cols, rows } });
  }

  kill(sessionId) {
    return this.call(MsgType.ReqKill, { session_id: sessionId });
  }

  restart(sessionId) {
    return this.call(MsgType.ReqRestart, { session_id: sessionId });
  }

  rename(sessionId, name) {
    return this.call(MsgType.ReqRename, { session_id: sessionId, name });
  }

  remove(sessionId) {
    return this.call(MsgType.ReqRemove, { session_id: sessionId });
  }

  subscribeList(fn) {
    this.listSubs.add(fn);
    this.list().then(fn).catch(() => {});
    return () => this.listSubs.delete(fn);
  }
}

let client = null;
let mainWindow = null;

function createWindow() {
  mainWindow = new BrowserWindow({
    width: 1000,
    height: 700,
    backgroundColor: '#1e1e1e',
    title: 'dispatch',
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
  });
  mainWindow.loadFile(path.join(__dirname, 'renderer', 'index.html'));

  // Surface renderer-side errors in the main-process log — otherwise a
  // thrown exception inside index.html's scripts is silent from here.
  mainWindow.webContents.on('console-message', (_e, level, message, line, sourceId) => {
    console.log(`[renderer] ${message} (${sourceId}:${line})`);
  });
  mainWindow.webContents.on('did-fail-load', (_e, code, desc) => {
    console.error(`[did-fail-load] ${code} ${desc}`);
  });
  mainWindow.webContents.on('preload-error', (_e, preloadPath, error) => {
    console.error(`[preload-error] ${preloadPath}:`, error);
  });
}

function registerIpcHandlers() {
  ipcMain.handle('list', () => client.list());
  ipcMain.handle('create', (_e, opts) => client.create(opts));
  ipcMain.handle('listAccounts', () => client.accounts());
  ipcMain.handle('attach', async (_e, { sessionId, cols, rows }) => {
    const scrollback = await client.attach(sessionId, cols, rows, (ev) => {
      if (!mainWindow) return;
      mainWindow.webContents.send('output', { sessionId, bytes: toU8(ev.bytes), exited: ev.exited });
    });
    return toU8(scrollback);
  });
  ipcMain.handle('detach', (_e, sessionId) => client.detach(sessionId));
  ipcMain.handle('write', (_e, sessionId, text) => client.write(sessionId, text));
  ipcMain.handle('resize', (_e, sessionId, cols, rows) => client.resize(sessionId, cols, rows));
  ipcMain.handle('kill', (_e, sessionId) => client.kill(sessionId));
  ipcMain.handle('restart', (_e, sessionId) => client.restart(sessionId));
  ipcMain.handle('rename', (_e, sessionId, name) => client.rename(sessionId, name));
  ipcMain.handle('remove', (_e, sessionId) => client.remove(sessionId));
  ipcMain.handle('homeDir', () => os.homedir());
  ipcMain.handle('detectAgents', () => detectAgents());
  ipcMain.handle('chooseDirectory', async () => {
    const result = await dialog.showOpenDialog(mainWindow, {
      properties: ['openDirectory', 'createDirectory'],
      defaultPath: os.homedir(),
    });
    if (result.canceled || !result.filePaths.length) return null;
    return result.filePaths[0];
  });
  ipcMain.handle('getRecents', () => loadRecents());
  ipcMain.handle('addRecent', (_e, entry) => addRecent(entry));
  ipcMain.handle('setBadgeCount', (_e, n) => {
    if (process.platform === 'darwin' && app.dock) {
      app.dock.setBadge(n > 0 ? String(n) : '');
    } else {
      app.setBadgeCount(n);
    }
  });
  ipcMain.handle('focusWindow', () => {
    if (!mainWindow) return;
    if (mainWindow.isMinimized()) mainWindow.restore();
    mainWindow.show();
    mainWindow.focus();
  });
}

app.whenReady().then(async () => {
  adoptLoginShellPath();
  const socketPath = defaultSocketPath();
  try {
    await ensureDaemonRunning(socketPath, 5000, 10);
    client = new SocketClient(socketPath);
    await client.connect();
  } catch (err) {
    dialog.showErrorBox('dispatch', `Could not connect to daemon:\n${err.message}`);
    app.quit();
    return;
  }

  registerIpcHandlers();
  createWindow();
  client.subscribeList((sessions) => {
    if (mainWindow) mainWindow.webContents.send('session-list', sessions);
  });
});

app.on('window-all-closed', () => {
  // Same philosophy as the TUI: closing the window never kills sessions
  // (they live in the daemon) — it only disconnects this client.
  app.quit();
});

