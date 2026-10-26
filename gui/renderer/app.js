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

const SPLIT_ROW_ICON = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3"><rect x="2" y="3" width="12" height="10" rx="1.5"/><path d="M8 3v10"/></svg>';
const SPLIT_COL_ICON = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3"><rect x="2" y="3" width="12" height="10" rx="1.5"/><path d="M2 8h12"/></svg>';
const ZOOM_ICON = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M9.5 2.5h4v4M6.5 13.5h-4v-4M13.5 2.5L9 7M2.5 13.5L7 9"/></svg>';
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
const attachWrap = document.getElementById('attach-wrap');
const paneParking = document.getElementById('pane-parking');
const bellBadge = document.getElementById('bell-badge');
const toastStack = document.getElementById('toast-stack');
const paletteScrim = document.getElementById('palette-scrim');
const paletteInput = document.getElementById('palette-input');
const paletteList = document.getElementById('palette-list');
const sheetScrim = document.getElementById('sheet-scrim');

// ---- state ----
let lastSessions = [];
let groupMode = 'flat';
const tabs = new Map(); // sessionId -> { id, name, term, fitAddon, container, offOutput, observer, size }
let activeTabId = null; // the focused pane
const prevStatusById = new Map();
let listInitialized = false;
let recents = [];
let paletteIndex = 0;
let paletteSplitDir = null; // 'row' | 'col' while the palette is picking a session for a new pane

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
    updatePaneHeads();
    restoreLayout(sessions);
  } catch { /* daemon not reachable yet — the push subscription will catch up */ }
}
bridge.onSessionList((sessions) => {
  trackAttentionAndReconcile(sessions);
  lastSessions = sessions;
  renderList();
  renderTabs();
  updatePaneHeads();
  restoreLayout(sessions);
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

async function relaunchRecent(entry, splitDir = null) {
  try {
    const id = await bridge.create({ name: entry.name, command: entry.command, args: entry.args, cwd: entry.cwd, cols: 80, rows: 24 });
    if (splitDir) openInSplit(id, entry.name, splitDir);
    else openOrActivateTab(id, entry.name);
  } catch (err) {
    alert('Failed to relaunch: ' + err.message);
  }
}

bridge.getRecents().then((r) => { recents = r; renderRecentStrip(); }).catch(() => {});

// =================================================================
// Attach view: open sessions ("tabs") stay attached in the background;
// the visible ones are tiled by a binary split tree whose leaves are
// session ids. Moving a session between panes just re-parents its live
// xterm — scrollback is never re-fetched.
//
//   leaf:  { type: 'leaf', id }
//   split: { type: 'split', dir: 'row' | 'col', ratio, a, b }
//          ('row' = side by side, 'col' = stacked; ratio is a's share)
// =================================================================

const LAYOUT_KEY = 'dispatch.layout.v1';
const MIN_RATIO = 0.15;

let layout = null;
let zoomedId = null;
let layoutRestored = false;

const leaf = (id) => ({ type: 'leaf', id });

function leafIds(node) {
  if (!node) return [];
  return node.type === 'leaf' ? [node.id] : [...leafIds(node.a), ...leafIds(node.b)];
}

function isVisible(id) {
  return leafIds(layout).includes(id);
}

// Returns a copy of node with the leaf for id replaced by fn(leaf).
function mapLeaf(node, id, fn) {
  if (!node) return node;
  if (node.type === 'leaf') return node.id === id ? fn(node) : node;
  return { ...node, a: mapLeaf(node.a, id, fn), b: mapLeaf(node.b, id, fn) };
}

// Returns a copy of node without the leaf for id; a split left with one
// child collapses into that child.
function removeLeaf(node, id) {
  if (!node) return null;
  if (node.type === 'leaf') return node.id === id ? null : node;
  const a = removeLeaf(node.a, id);
  const b = removeLeaf(node.b, id);
  if (!a) return b;
  if (!b) return a;
  return { ...node, a, b };
}

function focusedLeafId() {
  return isVisible(activeTabId) ? activeTabId : leafIds(layout)[0];
}

function saveLayout() {
  try {
    localStorage.setItem(LAYOUT_KEY, JSON.stringify({ layout, focused: activeTabId, zoomed: zoomedId }));
  } catch { /* storage unavailable — layout just won't survive a restart */ }
}

function loadLayout() {
  try { return JSON.parse(localStorage.getItem(LAYOUT_KEY) || 'null'); } catch { return null; }
}

// Re-opens the panes from the last run, dropping any session that has
// since exited or been removed. Runs once, on the first session list.
async function restoreLayout(sessions) {
  if (layoutRestored) return;
  layoutRestored = true;
  const saved = loadLayout();
  if (!saved || !saved.layout) return;
  const byId = new Map(sessions.map((s) => [s.id, s]));
  let tree = saved.layout;
  try {
    for (const id of leafIds(tree)) {
      if (!byId.has(id) || byId.get(id).status === 'exited') tree = removeLeaf(tree, id);
    }
  } catch { return; } // malformed saved layout
  if (!tree) return;
  for (const id of leafIds(tree)) await createTab(id, byId.get(id).name);
  layout = tree;
  activeTabId = isVisible(saved.focused) ? saved.focused : leafIds(layout)[0];
  zoomedId = isVisible(saved.zoomed) ? saved.zoomed : null;
  switchToAttachView();
  commitLayout();
}

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

// Opens id in a new pane next to the focused one. A session that is
// already on screen is just focused — it can't occupy two panes.
async function openInSplit(id, name, dir) {
  switchToAttachView();
  if (!tabs.has(id)) await createTab(id, name);
  if (!layout || isVisible(id)) { activateTab(id); return; }
  zoomedId = null;
  layout = mapLeaf(layout, focusedLeafId(), (l) => ({ type: 'split', dir, ratio: 0.5, a: l, b: leaf(id) }));
  activeTabId = id;
  commitLayout();
}

async function createTab(id, name) {
  const container = document.createElement('div');
  container.className = 'term-panel';
  container.dataset.id = id;
  container.innerHTML = `
    <div class="term-head">
      <span class="pane-dot"></span>
      <span class="pane-label">${escapeHtml(name)}</span>
      <span class="pane-status"></span>
      <span class="pane-actions">
        <button class="pane-btn" data-pane-act="split-row" title="Split right (&#8984;D)">${SPLIT_ROW_ICON}</button>
        <button class="pane-btn" data-pane-act="split-col" title="Split down (&#8984;&#8679;D)">${SPLIT_COL_ICON}</button>
        <button class="pane-btn" data-pane-act="zoom" title="Zoom pane (&#8984;&#8679;&#8629;)">${ZOOM_ICON}</button>
        <button class="pane-btn" data-pane-act="close-pane" title="Close pane — session stays open as a tab (&#8984;&#8679;W)">${CLOSE_ICON}</button>
      </span>
    </div>
    <div class="term-body-wrap"></div>`;
  paneParking.appendChild(container);

  const body = container.querySelector('.term-body-wrap');
  const term = new Terminal({ cursorBlink: true, fontSize: 13, fontFamily: TERM_FONT, theme: TERM_THEME });
  const fitAddon = new FitAddon.FitAddon();
  term.loadAddon(fitAddon);
  term.open(body);

  term.onData((data) => bridge.write(id, data));
  term.textarea?.addEventListener('focus', () => focusPane(id, { moveFocus: false }));
  const offOutput = bridge.onOutput((payload) => {
    if (payload.sessionId !== id) return;
    if (payload.bytes && payload.bytes.length) term.write(new Uint8Array(payload.bytes));
    if (payload.exited) term.write('\r\n\x1b[2m[process exited]\x1b[0m\r\n');
  });

  const tab = { id, name, term, fitAddon, container, offOutput, size: null, fitQueued: false };
  // One observer per pane covers window resizes, divider drags, zoom and
  // re-tiling alike; parked (hidden) panes measure 0 and are skipped.
  tab.observer = new ResizeObserver(() => scheduleFit(tab));
  tab.observer.observe(body);
  tabs.set(id, tab);

  try {
    const scrollback = await bridge.attach(id, 80, 24);
    if (scrollback && scrollback.length) term.write(new Uint8Array(scrollback));
  } catch (err) {
    term.write(`\r\n\x1b[31m[failed to attach: ${err.message}]\x1b[0m\r\n`);
  }
}

function scheduleFit(tab) {
  if (tab.fitQueued) return;
  tab.fitQueued = true;
  requestAnimationFrame(() => {
    tab.fitQueued = false;
    if (!tabs.has(tab.id) || tab.container.clientWidth === 0) return;
    tab.fitAddon.fit();
    const { cols, rows } = tab.term;
    if (tab.size && tab.size.cols === cols && tab.size.rows === rows) return;
    tab.size = { cols, rows };
    bridge.resize(tab.id, cols, rows);
  });
}

// Shows id: focuses its pane if it's on screen, otherwise swaps it into
// the focused pane (the session it replaces stays open as a tab).
function activateTab(id) {
  if (!tabs.has(id)) return;
  if (!layout) layout = leaf(id);
  else if (!isVisible(id)) layout = mapLeaf(layout, focusedLeafId(), () => leaf(id));
  if (zoomedId) zoomedId = id;
  activeTabId = id;
  commitLayout();
}

// Moves keyboard focus between panes without rebuilding the DOM.
function focusPane(id, { moveFocus = true } = {}) {
  if (!isVisible(id)) return;
  if (activeTabId !== id) {
    activeTabId = id;
    renderTabs();
    updatePaneHeads();
    saveLayout();
  }
  if (moveFocus) tabs.get(id)?.term.focus();
}

// Takes a pane off screen; its session stays attached as a background tab.
function closePane(id) {
  if (!isVisible(id)) return;
  layout = removeLeaf(layout, id);
  if (zoomedId === id) zoomedId = null;
  if (activeTabId === id) activeTabId = leafIds(layout)[0] ?? null;
  if (!layout) {
    showListView();
    renderTabs();
    renderLayout();
    saveLayout();
    return;
  }
  commitLayout();
}

async function closeTab(id) {
  const tab = tabs.get(id);
  if (!tab) return;
  tab.offOutput();
  tab.observer.disconnect();
  tab.term.dispose();
  tab.container.remove();
  tabs.delete(id);
  await bridge.detach(id).catch(() => {});
  layout = removeLeaf(layout, id);
  if (zoomedId === id) zoomedId = null;
  if (activeTabId === id) {
    const remaining = Array.from(tabs.keys());
    activeTabId = leafIds(layout)[0] ?? remaining[remaining.length - 1] ?? null;
    if (activeTabId && !layout) layout = leaf(activeTabId);
    if (!activeTabId) showListView();
  }
  commitLayout();
}

function toggleZoom() {
  if (leafIds(layout).length < 2 || !activeTabId) return;
  zoomedId = zoomedId ? null : activeTabId;
  commitLayout();
}

// Moves focus to the nearest pane in a screen direction.
function focusDirection(dir) {
  const cur = tabs.get(activeTabId);
  if (!cur || zoomedId) return;
  const from = cur.container.getBoundingClientRect();
  const cx = from.left + from.width / 2;
  const cy = from.top + from.height / 2;
  let best = null;
  let bestDist = Infinity;
  for (const id of leafIds(layout)) {
    if (id === activeTabId) continue;
    const r = tabs.get(id).container.getBoundingClientRect();
    const inDir = dir === 'left' ? r.right <= from.left + 1
      : dir === 'right' ? r.left >= from.right - 1
      : dir === 'up' ? r.bottom <= from.top + 1
      : r.top >= from.bottom - 1;
    if (!inDir) continue;
    const d = Math.hypot(r.left + r.width / 2 - cx, r.top + r.height / 2 - cy);
    if (d < bestDist) { best = id; bestDist = d; }
  }
  if (best) focusPane(best);
}

function commitLayout() {
  renderTabs();
  renderLayout();
  saveLayout();
  tabs.get(activeTabId)?.term.focus();
}

function renderLayout() {
  for (const tab of tabs.values()) {
    tab.container.style.flex = '';
    paneParking.appendChild(tab.container);
  }
  attachWrap.replaceChildren();
  const root = zoomedId && isVisible(zoomedId) ? leaf(zoomedId) : layout;
  if (root) attachWrap.appendChild(buildNode(root));
  attachWrap.classList.toggle('is-zoomed', !!zoomedId);
  attachWrap.classList.toggle('is-tiled', leafIds(layout).length > 1);
  updatePaneHeads();
}

// Pane tips popover: anchored under its button, right-aligned with it.
const paneHelpBtn = document.getElementById('btn-pane-help');
const paneHelp = document.getElementById('pane-help');
function showPaneHelp() {
  const r = paneHelpBtn.getBoundingClientRect();
  paneHelp.style.top = `${r.bottom + 8}px`;
  paneHelp.style.right = `${Math.max(8, window.innerWidth - r.right)}px`;
  paneHelp.classList.add('open');
}
function hidePaneHelp() { paneHelp.classList.remove('open'); }
paneHelpBtn.addEventListener('mouseenter', showPaneHelp);
paneHelpBtn.addEventListener('mouseleave', hidePaneHelp);
paneHelpBtn.addEventListener('focus', showPaneHelp);
paneHelpBtn.addEventListener('blur', hidePaneHelp);

function buildNode(node) {
  if (node.type === 'leaf') return tabs.get(node.id).container;
  const el = document.createElement('div');
  el.className = `split split-${node.dir}`;
  const a = buildNode(node.a);
  const b = buildNode(node.b);
  a.style.flex = `${node.ratio} 1 0`;
  b.style.flex = `${1 - node.ratio} 1 0`;
  const divider = document.createElement('div');
  divider.className = 'divider';
  divider.addEventListener('pointerdown', (e) => startDividerDrag(e, el, node, a, b));
  el.append(a, divider, b);
  return el;
}

function startDividerDrag(e, splitEl, node, a, b) {
  e.preventDefault();
  const rect = splitEl.getBoundingClientRect();
  document.body.classList.add(node.dir === 'row' ? 'is-resizing-row' : 'is-resizing-col');
  const onMove = (ev) => {
    const r = node.dir === 'row' ? (ev.clientX - rect.left) / rect.width : (ev.clientY - rect.top) / rect.height;
    node.ratio = Math.min(1 - MIN_RATIO, Math.max(MIN_RATIO, r));
    a.style.flex = `${node.ratio} 1 0`;
    b.style.flex = `${1 - node.ratio} 1 0`;
  };
  const onUp = () => {
    window.removeEventListener('pointermove', onMove);
    window.removeEventListener('pointerup', onUp);
    document.body.classList.remove('is-resizing-row', 'is-resizing-col');
    saveLayout();
  };
  window.addEventListener('pointermove', onMove);
  window.addEventListener('pointerup', onUp);
}

function updatePaneHeads() {
  const tiled = leafIds(layout).length > 1 && !zoomedId;
  for (const [id, tab] of tabs) {
    const s = lastSessions.find((x) => x.id === id);
    const status = s ? s.status : 'exited';
    tab.container.querySelector('.pane-dot').className = `pane-dot ${statusDotClass(status)}`;
    tab.container.querySelector('.pane-status').textContent =
      status === 'running' ? 'running' : status === 'needs-attention' ? 'needs input' : 'exited';
    tab.container.classList.toggle('focused', tiled && id === activeTabId);
    tab.container.classList.toggle('needs-attention', status === 'needs-attention' && id !== activeTabId);
    tab.container.querySelector('[data-pane-act="zoom"]').classList.toggle('on', zoomedId === id);
  }
}

function statusDotClass(status) {
  return status === 'running' ? 'good' : status === 'needs-attention' ? 'warn' : 'neutral';
}

function renderTabs() {
  tabstrip.querySelectorAll('.tab').forEach((t) => t.remove());
  const spacer = tabstrip.querySelector('.spacer');
  let i = 0;
  for (const [id, tab] of tabs) {
    i++;
    const s = lastSessions.find((x) => x.id === id);
    const status = s ? s.status : 'exited';
    const isActive = id === activeTabId;
    const isOnScreen = isVisible(id) && !isActive;
    const hasAttention = status === 'needs-attention' && !isActive;
    const el = document.createElement('div');
    el.className = ['tab', isActive && 'active', isOnScreen && 'on-screen', hasAttention && 'has-attention'].filter(Boolean).join(' ');
    el.dataset.tab = id;
    el.innerHTML = `
      <span class="tab-dot ${statusDotClass(status)}"></span>
      <span class="tab-name">${escapeHtml(tab.name)}</span>
      ${i <= 9 ? `<span class="tab-kbd">&#8984;${i}</span>` : ''}
      <span class="tab-actions">
        ${isVisible(id) ? '' : `<button class="tab-icon-btn" data-split="${id}" title="Open in a split to the right">${SPLIT_ROW_ICON}</button>`}
        <button class="tab-icon-btn" data-detach="${id}" title="Detach">${CLOSE_ICON}</button>
      </span>`;
    tabstrip.insertBefore(el, spacer);
  }
}

tabstrip.addEventListener('click', (e) => {
  const splitBtn = e.target.closest('[data-split]');
  if (splitBtn) {
    const tab = tabs.get(splitBtn.dataset.split);
    if (tab) openInSplit(tab.id, tab.name, 'row');
    return;
  }
  const detachBtn = e.target.closest('[data-detach]');
  if (detachBtn) { closeTab(detachBtn.dataset.detach); return; }
  const tabEl = e.target.closest('.tab');
  if (tabEl) activateTab(tabEl.dataset.tab);
});
document.getElementById('btn-split-row').addEventListener('click', () => openPalette({ splitDir: 'row' }));
document.getElementById('btn-split-col').addEventListener('click', () => openPalette({ splitDir: 'col' }));

attachWrap.addEventListener('click', (e) => {
  const btn = e.target.closest('[data-pane-act]');
  if (!btn) return;
  const id = btn.closest('.term-panel').dataset.id;
  const act = btn.dataset.paneAct;
  focusPane(id, { moveFocus: false });
  if (act === 'split-row' || act === 'split-col') openPalette({ splitDir: act === 'split-row' ? 'row' : 'col' });
  else if (act === 'zoom') toggleZoom();
  else if (act === 'close-pane') closePane(id);
});

// =================================================================
// Command palette (⌘K)
// =================================================================

function paletteEntries() {
  const entries = [];
  const seen = new Set();
  if (paletteSplitDir) entries.push({ kind: 'new', name: 'New session…', path: 'launch into the new pane' });
  for (const s of lastSessions) {
    if (paletteSplitDir && s.id === activeTabId) continue; // can't split a pane with itself
    entries.push({ kind: 'session', id: s.id, name: s.name, path: s.cwd, status: s.status });
    seen.add(s.name);
  }
  for (const r of recents) {
    if (!seen.has(r.name)) entries.push({ kind: 'recent', name: r.name, path: `${r.cwd} · relaunch`, entry: r });
  }
  return entries;
}

function dotColorFor(e) {
  if (e.kind === 'recent' || e.kind === 'new') return 'var(--text-dim)';
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
  const dir = paletteSplitDir;
  closePalette({ refocus: false });
  if (entry.kind === 'new') openNewSessionModal({ splitDir: dir });
  else if (entry.kind === 'session') dir ? openInSplit(entry.id, entry.name, dir) : openOrActivateTab(entry.id, entry.name);
  else relaunchRecent(entry.entry, dir);
}

function openPalette({ splitDir = null } = {}) {
  paletteSplitDir = splitDir;
  paletteInput.placeholder = splitDir ? `Open in a split ${splitDir === 'row' ? 'to the right' : 'below'}…` : 'Jump to a session…';
  paletteScrim.classList.add('open');
  paletteInput.value = '';
  renderPaletteList('');
  setTimeout(() => paletteInput.focus(), 10);
}
function closePalette({ refocus = true } = {}) {
  if (!paletteScrim.classList.contains('open')) return;
  paletteScrim.classList.remove('open');
  paletteSplitDir = null;
  if (refocus && !viewAttach.classList.contains('hidden')) tabs.get(activeTabId)?.term.focus();
}

document.querySelectorAll('#btn-palette-open, #btn-palette-open-2').forEach((b) => b.addEventListener('click', () => openPalette()));
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
    if (activeTabId) { e.preventDefault(); e.shiftKey ? closePane(activeTabId) : closeTab(activeTabId); }
  } else if (e.key.toLowerCase() === 'd' && e.metaKey) {
    // ⌘ only: ctrl+D has to keep reaching the terminal as EOF.
    e.preventDefault();
    openPalette({ splitDir: e.shiftKey ? 'col' : 'row' });
  } else if (e.key === 'Enter' && e.shiftKey) {
    e.preventDefault();
    toggleZoom();
  } else if (e.altKey && e.key.startsWith('Arrow')) {
    e.preventDefault();
    focusDirection(e.key.slice(5).toLowerCase());
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
let pendingSplitDir = null; // set when the modal was opened to fill a new pane

async function openNewSessionModal({ splitDir = null } = {}) {
  pendingSplitDir = splitDir;
  nameInput.value = '';
  cwdInput.value = await bridge.homeDir();
  availableAgents = await bridge.detectAgents();
  agentSelect.innerHTML = availableAgents.length
    ? availableAgents.map((a) => `<option value="${a.id}">${escapeHtml(a.label)}</option>`).join('')
    : '<option disabled>No agent CLIs found on PATH</option>';
  modal.classList.remove('hidden');
  nameInput.focus();
}
document.getElementById('btn-new').addEventListener('click', () => openNewSessionModal());
document.getElementById('btn-new-empty').addEventListener('click', () => openNewSessionModal());
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
    if (pendingSplitDir) openInSplit(id, name, pendingSplitDir);
    else openOrActivateTab(id, name);
  } catch (err) {
    alert('Failed to create session: ' + err.message);
  }
});
