// Renderer: vanilla DOM, no framework. Talks only to window.dispatch (exposed
// by preload.js) — never to Node or the socket directly.
//
// Named `bridge`, not `dispatch`: contextBridge.exposeInMainWorld defines
// window.dispatch as non-configurable, and `const dispatch = window.dispatch`
// collides with that at parse time (SyntaxError) — which silently kills the
// entire script. Keep this name distinct.
const bridge = window.dispatch;

const TERM_FONT = 'ui-monospace, "SF Mono", "Cascadia Code", "JetBrains Mono", Menlo, Consolas, monospace';
const TERM_THEME = { background: '#0e0d11', foreground: '#d8d4cf', cursor: '#d8d4cf' };

const PIN_ICON = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M9.5 2.5l4 4-2 2-1 3.5-1.5-1.5L5 14l-1-1 3.5-4L6 7.5l3.5-5z"/></svg>';
const CLOSE_ICON = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"><path d="M4 4l8 8M12 4l-8 8"/></svg>';
const RESTART_ICON = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M13 8A5 5 0 113.5 5.5M3.5 5.5V2.5M3.5 5.5h3"/></svg>';
const FOLDER_ICON = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><path d="M2 4.5a1 1 0 011-1h3l1.2 1.5H13a1 1 0 011 1V11a1 1 0 01-1 1H3a1 1 0 01-1-1V4.5z"/></svg>';

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function relTime(iso) {
  const d = (Date.now() - new Date(iso).getTime()) / 1000;
  if (d < 60) return 'started just now';
  if (d < 3600) return `started ${Math.floor(d / 60)}m ago`;
  if (d < 86400) return `started ${Math.floor(d / 3600)}h ago`;
  return `started ${Math.floor(d / 86400)}d ago`;
}

// ---- DOM refs ----
const viewList = document.getElementById('view-list');
const viewAttach = document.getElementById('view-attach');
const recentStrip = document.getElementById('recent-strip');
const listToolbar = document.getElementById('list-toolbar');
const sessionCount = document.getElementById('session-count');
const listFlat = document.getElementById('list-flat');
const listGrouped = document.getElementById('list-grouped');
const emptyState = document.getElementById('empty-state');
const tabstrip = document.getElementById('tabstrip');
const pinHint = document.getElementById('pin-hint');
const attachWrap = document.getElementById('attach-wrap');
const bellBadge = document.getElementById('bell-badge');
const toastStack = document.getElementById('toast-stack');
const paletteScrim = document.getElementById('palette-scrim');
const paletteInput = document.getElementById('palette-input');
const paletteList = document.getElementById('palette-list');
const sheetScrim = document.getElementById('sheet-scrim');

// ---- state ----
let lastSessions = [];
let groupMode = 'flat';
const tabs = new Map(); // sessionId -> { id, name, term, fitAddon, container, offOutput }
let activeTabId = null;
let pinnedTabId = null;
const prevStatusById = new Map();
let listInitialized = false;
let recents = [];
let paletteIndex = 0;

// =================================================================
// Session list: render, actions, attention tracking
// =================================================================

function statusBadge(status) {
  if (status === 'running') return '<span class="badge badge-running"><i class="dot"></i>Running</span>';
  if (status === 'needs-attention') return '<span class="badge badge-attention"><i class="dot"></i>Needs attention</span>';
  return '<span class="badge badge-exited"><i class="dot"></i>Exited</span>';
}

function rowHtml(s, { showCwd = true } = {}) {
  const rowCls = ['row', 'row-body', s.status === 'needs-attention' ? 'is-attention' : '', s.status === 'exited' ? 'is-exited' : '']
    .filter(Boolean).join(' ');
  const actions = s.status === 'exited'
    ? `<button class="btn btn-ghost btn-sm" data-act="restart" data-id="${s.id}">Restart</button>
       <button class="btn btn-danger-ghost btn-sm" data-act="remove" data-id="${s.id}">Remove</button>
       <button class="btn btn-ghost btn-sm" data-act="rename" data-id="${s.id}">Rename</button>`
    : `<button class="btn btn-primary btn-sm" data-act="attach" data-id="${s.id}">Attach</button>
       <button class="btn btn-ghost btn-sm" data-act="restart" data-id="${s.id}">Restart</button>
       <button class="btn btn-danger-ghost btn-sm" data-act="kill" data-id="${s.id}">Kill</button>
       <button class="btn btn-ghost btn-sm" data-act="rename" data-id="${s.id}">Rename</button>`;
  return `
    <div class="${rowCls}" data-id="${s.id}">
      <div class="cell-name"><span class="name">${escapeHtml(s.name)}</span><span class="agent-sub">${relTime(s.started_at)}</span></div>
      <div>${statusBadge(s.status)}</div>
      <div class="cell-agent">${escapeHtml(s.command)}</div>
      <div class="cell-cwd">${showCwd ? escapeHtml(s.cwd) : '&nbsp;'}</div>
      <div class="cell-actions">${actions}</div>
    </div>`;
}

function flatHtml(sessions) {
  return `<div class="table">
    <div class="row row-head"><div>Name</div><div>Status</div><div>Agent</div><div>Directory</div><div></div></div>
    ${sessions.map((s) => rowHtml(s)).join('')}
  </div>`;
}

function groupedHtml(sessions) {
  const groups = new Map();
  for (const s of sessions) {
    if (!groups.has(s.cwd)) groups.set(s.cwd, []);
    groups.get(s.cwd).push(s);
  }
  let html = '';
  for (const [cwd, list] of groups) {
    html += `<div class="project-group">
      <div class="project-group-head">${FOLDER_ICON}${escapeHtml(cwd)}</div>
      <div class="table">${list.map((s) => rowHtml(s, { showCwd: false })).join('')}</div>
    </div>`;
  }
  return html;
}

function renderList() {
  const sessions = lastSessions;
  sessionCount.textContent = `${sessions.length} session${sessions.length === 1 ? '' : 's'}`;
  listToolbar.classList.toggle('hidden', sessions.length === 0);
  emptyState.classList.toggle('hidden', sessions.length > 0);
  listFlat.classList.toggle('hidden', sessions.length === 0 || groupMode !== 'flat');
  listGrouped.classList.toggle('hidden', sessions.length === 0 || groupMode !== 'project');
  if (sessions.length > 0) {
    listFlat.innerHTML = flatHtml(sessions);
    listGrouped.innerHTML = groupedHtml(sessions);
  }
  const needingAttention = sessions.filter((s) => s.status === 'needs-attention').length;
  bellBadge.textContent = String(needingAttention);
  bellBadge.classList.toggle('hidden', needingAttention === 0);
  bridge.setBadgeCount(needingAttention);
}

function setGroupMode(mode) {
  groupMode = mode;
  document.getElementById('btn-group-flat').classList.toggle('active', mode === 'flat');
  document.getElementById('btn-group-project').classList.toggle('active', mode === 'project');
  renderList();
}
document.getElementById('btn-group-flat').addEventListener('click', () => setGroupMode('flat'));
document.getElementById('btn-group-project').addEventListener('click', () => setGroupMode('project'));

viewList.addEventListener('click', (e) => {
  const btn = e.target.closest('[data-act]');
  if (!btn) return;
  const id = btn.dataset.id;
  const act = btn.dataset.act;
  const s = lastSessions.find((x) => x.id === id);
  if (!s) return;
  if (act === 'attach') openOrActivateTab(id, s.name);
  else if (act === 'restart') bridge.restart(id).catch((err) => alert(err.message));
  else if (act === 'kill') { if (confirm(`Kill session "${s.name}"?`)) bridge.kill(id).catch((err) => alert(err.message)); }
  else if (act === 'remove') { if (confirm(`Remove exited session "${s.name}"?`)) bridge.remove(id).catch((err) => alert(err.message)); }
  else if (act === 'rename') {
    const name = prompt('New name', s.name);
    if (name && name.trim()) bridge.rename(id, name.trim()).catch((err) => alert(err.message));
  }
});

// ---- notifications on a needs-attention transition ----
function notifyAttention(session) {
  try {
    const n = new Notification(`${session.name} needs your input`, {
      body: `${session.command} has gone idle — it's probably waiting on you.`,
    });
    n.onclick = async () => { await bridge.focusWindow(); openOrActivateTab(session.id, session.name); };
  } catch { /* Notification API unavailable in this environment */ }
  showToast(session);
}

function showToast(session) {
  const toast = document.createElement('div');
  toast.className = 'toast';
  toast.innerHTML = `<span class="tdot2"></span><div><p class="ttitle">${escapeHtml(session.name)} needs your input</p><p class="tbody">${escapeHtml(session.command)} has gone idle.</p></div>`;
  toast.addEventListener('click', () => { openOrActivateTab(session.id, session.name); toast.remove(); });
  toastStack.appendChild(toast);
  requestAnimationFrame(() => toast.classList.add('show'));
  setTimeout(() => {
    toast.classList.remove('show');
    setTimeout(() => toast.remove(), 200);
  }, 4500);
}

function trackAttentionAndReconcile(sessions) {
  for (const s of sessions) {
    const prev = prevStatusById.get(s.id);
    if (listInitialized && prev !== 'needs-attention' && s.status === 'needs-attention') notifyAttention(s);
    prevStatusById.set(s.id, s.status);
  }
  const liveIds = new Set(sessions.map((s) => s.id));
  for (const id of Array.from(prevStatusById.keys())) if (!liveIds.has(id)) prevStatusById.delete(id);
  for (const id of Array.from(tabs.keys())) if (!liveIds.has(id)) closeTab(id); // session removed from the registry entirely
  listInitialized = true;
}

async function refreshList() {
  try {
    const sessions = await bridge.list();
    trackAttentionAndReconcile(sessions);
    lastSessions = sessions;
    renderList();
    renderTabs();
  } catch { /* daemon not reachable yet — the push subscription will catch up */ }
}
bridge.onSessionList((sessions) => {
  trackAttentionAndReconcile(sessions);
  lastSessions = sessions;
  renderList();
  renderTabs();
});
refreshList();

document.getElementById('btn-bell').addEventListener('click', openPalette);

// =================================================================
// Recent / quick-launch strip
// =================================================================

function renderRecentStrip() {
  recentStrip.classList.toggle('hidden', recents.length === 0);
  recentStrip.innerHTML = recents.map((r) => `
    <div class="recent-pill" data-recent="${escapeHtml(r.name)}">
      <span class="pdot"></span>${escapeHtml(r.name)}${RESTART_ICON}
    </div>`).join('');
}
recentStrip.addEventListener('click', (e) => {
  const pill = e.target.closest('[data-recent]');
  if (!pill) return;
  const entry = recents.find((r) => r.name === pill.dataset.recent);
  if (entry) relaunchRecent(entry);
});

async function relaunchRecent(entry) {
  try {
    const id = await bridge.create({ name: entry.name, command: entry.command, args: entry.args, cwd: entry.cwd, cols: 80, rows: 24 });
    openOrActivateTab(id, entry.name);
  } catch (err) {
    alert('Failed to relaunch: ' + err.message);
  }
}

bridge.getRecents().then((r) => { recents = r; renderRecentStrip(); }).catch(() => {});

// =================================================================
// Attach view: multi-tab terminal manager (open tabs stay attached
// in the background — switching tabs never re-fetches scrollback,
// it's just a show/hide of already-live xterm instances).
// =================================================================

function switchToAttachView() {
  viewList.classList.add('hidden');
  viewAttach.classList.remove('hidden');
}
function showListView() {
  viewAttach.classList.add('hidden');
  viewList.classList.remove('hidden');
}
document.getElementById('btn-back').addEventListener('click', showListView);

async function openOrActivateTab(id, name) {
  switchToAttachView();
  if (!tabs.has(id)) await createTab(id, name);
  activateTab(id);
}

async function createTab(id, name) {
  const container = document.createElement('div');
  container.className = 'term-panel';
  container.innerHTML = `
    <div class="term-head">
      <span class="pane-label">${escapeHtml(name)}</span>
      <button class="unpin-btn" data-unpin="${id}">UNPIN</button>
    </div>
    <div class="term-body-wrap"></div>`;
  attachWrap.appendChild(container);

  const term = new Terminal({ cursorBlink: true, fontSize: 13, fontFamily: TERM_FONT, theme: TERM_THEME });
  const fitAddon = new FitAddon.FitAddon();
  term.loadAddon(fitAddon);
  term.open(container.querySelector('.term-body-wrap'));

  term.onData((data) => bridge.write(id, data));
  const offOutput = bridge.onOutput((payload) => {
    if (payload.sessionId !== id) return;
    if (payload.bytes && payload.bytes.length) term.write(new Uint8Array(payload.bytes));
    if (payload.exited) term.write('\r\n\x1b[2m[process exited]\x1b[0m\r\n');
  });

  tabs.set(id, { id, name, term, fitAddon, container, offOutput });

  try {
    const scrollback = await bridge.attach(id, 80, 24);
    if (scrollback && scrollback.length) term.write(new Uint8Array(scrollback));
  } catch (err) {
    term.write(`\r\n\x1b[31m[failed to attach: ${err.message}]\x1b[0m\r\n`);
  }
}

function activateTab(id) {
  if (!tabs.has(id)) return;
  activeTabId = id;
  if (pinnedTabId === id) pinnedTabId = null; // can't pin a tab alongside itself
  renderTabs();
  layoutPanes();
}

function togglePin(id) {
  if (id === activeTabId || !tabs.has(id)) return;
  pinnedTabId = pinnedTabId === id ? null : id;
  renderTabs();
  layoutPanes();
}

async function closeTab(id) {
  const tab = tabs.get(id);
  if (!tab) return;
  tab.offOutput();
  tab.term.dispose();
  tab.container.remove();
  tabs.delete(id);
  await bridge.detach(id).catch(() => {});
  if (pinnedTabId === id) pinnedTabId = null;
  if (activeTabId === id) {
    const remaining = Array.from(tabs.keys());
    activeTabId = remaining.length ? remaining[remaining.length - 1] : null;
    if (!activeTabId) showListView();
  }
  renderTabs();
  layoutPanes();
}

function renderTabs() {
  tabstrip.querySelectorAll('.tab').forEach((t) => t.remove());
  const spacer = tabstrip.querySelector('.spacer');
  let i = 0;
  for (const [id, tab] of tabs) {
    i++;
    const s = lastSessions.find((x) => x.id === id);
    const status = s ? s.status : 'exited';
    const dotCls = status === 'running' ? 'good' : status === 'needs-attention' ? 'warn' : 'neutral';
    const isActive = id === activeTabId;
    const isPinned = id === pinnedTabId;
    const hasAttention = status === 'needs-attention' && !isActive;
    const el = document.createElement('div');
    el.className = ['tab', isActive && 'active', isPinned && 'pinned', hasAttention && 'has-attention'].filter(Boolean).join(' ');
    el.dataset.tab = id;
    el.innerHTML = `
      <span class="tab-dot ${dotCls}"></span>
      <span class="tab-name">${escapeHtml(tab.name)}</span>
      ${i <= 9 ? `<span class="tab-kbd">&#8984;${i}</span>` : ''}
      <span class="tab-actions">
        ${isActive ? '' : `<button class="tab-icon-btn pin-btn" data-pin="${id}" title="Pin alongside the active tab">${PIN_ICON}</button>`}
        <button class="tab-icon-btn" data-detach="${id}" title="Detach">${CLOSE_ICON}</button>
      </span>`;
    tabstrip.insertBefore(el, spacer);
  }
}

tabstrip.addEventListener('click', (e) => {
  const pinBtn = e.target.closest('[data-pin]');
  if (pinBtn) { togglePin(pinBtn.dataset.pin); return; }
  const detachBtn = e.target.closest('[data-detach]');
  if (detachBtn) { closeTab(detachBtn.dataset.detach); return; }
  const tabEl = e.target.closest('.tab');
  if (tabEl) activateTab(tabEl.dataset.tab);
});
attachWrap.addEventListener('click', (e) => {
  const unpinBtn = e.target.closest('[data-unpin]');
  if (unpinBtn) { pinnedTabId = null; renderTabs(); layoutPanes(); }
});

function layoutPanes() {
  for (const tab of tabs.values()) {
    tab.container.classList.remove('primary', 'secondary');
    tab.container.style.display = 'none';
  }
  const active = activeTabId ? tabs.get(activeTabId) : null;
  const pinned = pinnedTabId ? tabs.get(pinnedTabId) : null;
  attachWrap.classList.toggle('is-split', !!pinned);
  pinHint.classList.toggle('hidden', !active || !!pinned);
  if (active) {
    active.container.classList.add('primary');
    active.container.style.display = 'flex';
    pinHint.innerHTML = `Click the pin icon on a background tab to view it side by side with <strong>${escapeHtml(active.name)}</strong>.`;
  }
  if (pinned) {
    pinned.container.classList.add('secondary');
    pinned.container.style.display = 'flex';
  }
  requestAnimationFrame(() => {
    if (active) { active.fitAddon.fit(); bridge.resize(active.id, active.term.cols, active.term.rows); }
    if (pinned) { pinned.fitAddon.fit(); bridge.resize(pinned.id, pinned.term.cols, pinned.term.rows); }
  });
}
window.addEventListener('resize', () => { if (!viewAttach.classList.contains('hidden')) layoutPanes(); });

// =================================================================
// Command palette (⌘K)
// =================================================================

function paletteEntries() {
  const entries = [];
  const seen = new Set();
  for (const s of lastSessions) {
    entries.push({ kind: 'session', id: s.id, name: s.name, path: s.cwd, status: s.status });
    seen.add(s.name);
  }
  for (const r of recents) {
    if (!seen.has(r.name)) entries.push({ kind: 'recent', name: r.name, path: `${r.cwd} · relaunch`, entry: r });
  }
  return entries;
}

function dotColorFor(e) {
  if (e.kind === 'recent') return 'var(--text-dim)';
  if (e.status === 'running') return 'var(--good)';
  if (e.status === 'needs-attention') return 'var(--warn)';
  return 'var(--text-dim)';
}

function renderPaletteList(filter) {
  const q = (filter || '').trim().toLowerCase();
  const entries = paletteEntries().filter((e) => !q || e.name.toLowerCase().includes(q) || e.path.toLowerCase().includes(q));
  paletteList._entries = entries;
  paletteIndex = 0;
  paletteList.innerHTML = entries.length
    ? entries.map((e, i) => `
      <div class="palette-item${i === 0 ? ' active-row' : ''}" data-index="${i}">
        <span class="pdot" style="background:${dotColorFor(e)}"></span>
        <span class="name">${escapeHtml(e.name)}</span>
        <span class="path">${escapeHtml(e.path)}</span>
      </div>`).join('')
    : '<div class="palette-empty">No matching sessions</div>';
}

function movePaletteSelection(delta) {
  const items = Array.from(paletteList.querySelectorAll('.palette-item'));
  if (!items.length) return;
  items[paletteIndex]?.classList.remove('active-row');
  paletteIndex = Math.max(0, Math.min(paletteIndex + delta, items.length - 1));
  items[paletteIndex].classList.add('active-row');
  items[paletteIndex].scrollIntoView({ block: 'nearest' });
}

function choosePaletteEntry(entry) {
  if (!entry) return;
  closePalette();
  if (entry.kind === 'session') openOrActivateTab(entry.id, entry.name);
  else relaunchRecent(entry.entry);
}

function openPalette() {
  paletteScrim.classList.add('open');
  paletteInput.value = '';
  renderPaletteList('');
  setTimeout(() => paletteInput.focus(), 10);
}
function closePalette() { paletteScrim.classList.remove('open'); }

document.querySelectorAll('#btn-palette-open, #btn-palette-open-2').forEach((b) => b.addEventListener('click', openPalette));
paletteScrim.addEventListener('click', (e) => { if (e.target === paletteScrim) closePalette(); });
paletteList.addEventListener('click', (e) => {
  const item = e.target.closest('.palette-item');
  if (item) choosePaletteEntry(paletteList._entries[Number(item.dataset.index)]);
});
paletteInput.addEventListener('input', () => renderPaletteList(paletteInput.value));
paletteInput.addEventListener('keydown', (e) => {
  if (e.key === 'ArrowDown') { e.preventDefault(); movePaletteSelection(1); }
  else if (e.key === 'ArrowUp') { e.preventDefault(); movePaletteSelection(-1); }
  else if (e.key === 'Enter') { e.preventDefault(); choosePaletteEntry(paletteList._entries[paletteIndex]); }
  else if (e.key === 'Escape') { closePalette(); }
});

// ---- shortcuts cheatsheet ----
document.querySelectorAll('#btn-shortcuts, #btn-shortcuts-2').forEach((b) => b.addEventListener('click', () => sheetScrim.classList.add('open')));
sheetScrim.addEventListener('click', (e) => { if (e.target === sheetScrim) sheetScrim.classList.remove('open'); });

// ---- global keyboard shortcuts ----
document.addEventListener('keydown', (e) => {
  const mod = e.metaKey || e.ctrlKey;
  if (mod && e.key.toLowerCase() === 'k') { e.preventDefault(); paletteScrim.classList.contains('open') ? closePalette() : openPalette(); return; }
  if (e.key === 'Escape') { closePalette(); sheetScrim.classList.remove('open'); return; }
  if (mod && e.key === '/') { e.preventDefault(); sheetScrim.classList.add('open'); return; }
  if (mod && e.key.toLowerCase() === 'n') { e.preventDefault(); openNewSessionModal(); return; }

  const attachVisible = !viewAttach.classList.contains('hidden');
  if (!mod || !attachVisible) return;
  if (/^[1-9]$/.test(e.key)) {
    const id = Array.from(tabs.keys())[Number(e.key) - 1];
    if (id) { e.preventDefault(); activateTab(id); }
  } else if (e.key === ']' || e.key === '[') {
    const ids = Array.from(tabs.keys());
    const idx = ids.indexOf(activeTabId);
    if (idx >= 0 && ids.length > 1) {
      e.preventDefault();
      activateTab(ids[e.key === ']' ? (idx + 1) % ids.length : (idx - 1 + ids.length) % ids.length]);
    }
  } else if (e.key.toLowerCase() === 'w') {
    if (activeTabId) { e.preventDefault(); closeTab(activeTabId); }
  }
});

// =================================================================
// New session modal
// =================================================================

const modal = document.getElementById('new-session-modal');
const nameInput = document.getElementById('ns-name');
const agentSelect = document.getElementById('ns-agent');
const cwdInput = document.getElementById('ns-cwd');
let availableAgents = [];

async function openNewSessionModal() {
  nameInput.value = '';
  cwdInput.value = await bridge.homeDir();
  availableAgents = await bridge.detectAgents();
  agentSelect.innerHTML = availableAgents.length
    ? availableAgents.map((a) => `<option value="${a.id}">${escapeHtml(a.label)}</option>`).join('')
    : '<option disabled>No agent CLIs found on PATH</option>';
  modal.classList.remove('hidden');
  nameInput.focus();
}
document.getElementById('btn-new').addEventListener('click', openNewSessionModal);
document.getElementById('btn-new-empty').addEventListener('click', openNewSessionModal);
document.getElementById('ns-browse').addEventListener('click', async () => {
  const dir = await bridge.chooseDirectory();
  if (dir) cwdInput.value = dir;
});
document.getElementById('ns-cancel').addEventListener('click', () => modal.classList.add('hidden'));
document.getElementById('ns-create').addEventListener('click', async () => {
  const name = nameInput.value.trim();
  const agent = availableAgents.find((a) => a.id === agentSelect.value);
  const cwd = cwdInput.value.trim();
  if (!name || !agent) { alert('Name and agent are required.'); return; }
  modal.classList.add('hidden');
  try {
    const id = await bridge.create({ name, command: agent.command, args: agent.args, cwd, cols: 80, rows: 24 });
    recents = await bridge.addRecent({ name, command: agent.command, args: agent.args, cwd });
    renderRecentStrip();
    openOrActivateTab(id, name);
  } catch (err) {
    alert('Failed to create session: ' + err.message);
  }
});
