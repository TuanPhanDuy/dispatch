// Minimal contextBridge surface: the renderer never touches Node or the
// socket directly, only this whitelisted API. Kept 1:1 with main.js's
// ipcMain handlers.
const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('dispatch', {
  list: () => ipcRenderer.invoke('list'),
  create: (opts) => ipcRenderer.invoke('create', opts),
  attach: (sessionId, cols, rows) => ipcRenderer.invoke('attach', { sessionId, cols, rows }),
  detach: (sessionId) => ipcRenderer.invoke('detach', sessionId),
  write: (sessionId, text) => ipcRenderer.invoke('write', sessionId, text),
  resize: (sessionId, cols, rows) => ipcRenderer.invoke('resize', sessionId, cols, rows),
  kill: (sessionId) => ipcRenderer.invoke('kill', sessionId),
  restart: (sessionId) => ipcRenderer.invoke('restart', sessionId),
  rename: (sessionId, name) => ipcRenderer.invoke('rename', sessionId, name),
  remove: (sessionId) => ipcRenderer.invoke('remove', sessionId),
  homeDir: () => ipcRenderer.invoke('homeDir'),
  detectAgents: () => ipcRenderer.invoke('detectAgents'),
  chooseDirectory: () => ipcRenderer.invoke('chooseDirectory'),
  getRecents: () => ipcRenderer.invoke('getRecents'),
  addRecent: (entry) => ipcRenderer.invoke('addRecent', entry),
  setBadgeCount: (n) => ipcRenderer.invoke('setBadgeCount', n),
  focusWindow: () => ipcRenderer.invoke('focusWindow'),

  onSessionList: (cb) => {
    const handler = (_e, sessions) => cb(sessions);
    ipcRenderer.on('session-list', handler);
    return () => ipcRenderer.removeListener('session-list', handler);
  },
  onOutput: (cb) => {
    const handler = (_e, payload) => cb(payload);
    ipcRenderer.on('output', handler);
    return () => ipcRenderer.removeListener('output', handler);
  },
});
