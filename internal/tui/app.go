package tui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tuanbin007/dispatch/internal/client"
	"github.com/tuanbin007/dispatch/internal/config"
)

type viewMode int

const (
	viewList viewMode = iota
	viewAttached
	viewNewSession
)

// AppModel is the root Bubble Tea model: it routes messages to whichever
// sub-view is active and owns the things none of them should own
// individually — the session-list subscription, and reconciling live
// sessions against persisted definitions in cfg.
type AppModel struct {
	manager client.SessionManager
	cfg     *config.Config

	mode   viewMode
	list   *ListModel
	attach *AttachModel
	wizard *NewSessionModel

	listCh         <-chan []client.SessionSummary
	lastLive       []client.SessionSummary
	sessionProfile map[string]string // live session ID -> profile name, for persisting defs correctly on rename

	width, height int
	toast         string
}

func NewAppModel(manager client.SessionManager, cfg *config.Config) *AppModel {
	return &AppModel{
		manager:        manager,
		cfg:            cfg,
		mode:           viewList,
		list:           NewListModel(),
		sessionProfile: make(map[string]string),
		width:          80,
		height:         24,
	}
}

func waitForSessionList(ch <-chan []client.SessionSummary) tea.Cmd {
	return func() tea.Msg {
		sessions, ok := <-ch
		if !ok {
			return nil
		}
		return sessionListMsg(sessions)
	}
}

func (m *AppModel) Init() tea.Cmd {
	m.listCh = m.manager.Subscribe()
	return waitForSessionList(m.listCh)
}

func (m *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.attach != nil {
			updated, cmd := m.attach.Update(msg)
			m.attach = updated
			return m, cmd
		}
		return m, nil

	case sessionListMsg:
		m.lastLive = []client.SessionSummary(msg)
		m.refreshList()
		return m, waitForSessionList(m.listCh)

	case attachRequestMsg:
		return m.openAttach(msg.id, msg.name)

	case backToListMsg:
		m.attach = nil
		m.mode = viewList
		return m, nil

	case newSessionRequestMsg:
		cwd, _ := os.Getwd()
		m.wizard = NewNewSessionModel(m.cfg.Profiles, cwd)
		m.mode = viewNewSession
		return m, nil

	case cancelWizardMsg:
		m.wizard = nil
		m.mode = viewList
		return m, nil

	case createSessionMsg:
		return m.createAndAttach(msg)

	case relaunchDefMsg:
		return m.relaunchDef(msg.def)

	case removeDefMsg:
		if msg.id != "" {
			_ = m.manager.Remove(msg.id)
		}
		m.cfg.RemoveSession(msg.name)
		_ = m.cfg.Save()
		m.refreshList()
		return m, nil

	case killRequestMsg:
		_ = m.manager.Kill(msg.id)
		return m, nil

	case restartRequestMsg:
		_ = m.manager.Restart(msg.id)
		return m, nil

	case renameSubmitMsg:
		m.applyRename(msg.row, msg.name)
		return m, nil

	case quitAppMsg:
		return m, tea.Quit
	}

	return m.dispatch(msg)
}

// refreshList recomputes the merged live+persisted rows from the last known
// live snapshot. Call this after any config mutation that doesn't also
// trigger a fresh sessionListMsg from the daemon (e.g. removing a
// stopped-only definition has no live-side effect to push one).
func (m *AppModel) refreshList() {
	m.list.SetRows(m.mergedRows(m.lastLive))
}

// mergedRows reconciles live sessions against persisted definitions,
// matched by name: a def with a matching live session renders as that live
// row; a def with none renders "stopped" (relaunchable); a live session
// with no matching def (e.g. created by another client) renders live,
// un-persisted.
func (m *AppModel) mergedRows(live []client.SessionSummary) []Row {
	byName := make(map[string]client.SessionSummary, len(live))
	for _, s := range live {
		byName[s.Name] = s
	}

	seen := make(map[string]bool, len(m.cfg.Sessions))
	rows := make([]Row, 0, len(live)+len(m.cfg.Sessions))
	for _, def := range m.cfg.Sessions {
		if s, ok := byName[def.Name]; ok {
			rows = append(rows, Row{Live: true, Summary: s, Def: def})
		} else {
			rows = append(rows, Row{Live: false, Def: def})
		}
		seen[def.Name] = true
	}
	for _, s := range live {
		if !seen[s.Name] {
			rows = append(rows, Row{Live: true, Summary: s})
		}
	}
	return rows
}

func (m *AppModel) openAttach(id, name string) (tea.Model, tea.Cmd) {
	attach, err := NewAttachModel(m.manager, id, name, max(m.width, 80), max(m.height, 24))
	if err != nil {
		m.toast = err.Error()
		return m, nil
	}
	m.attach = attach
	m.mode = viewAttached
	return m, m.attach.Init()
}

func (m *AppModel) createAndAttach(msg createSessionMsg) (tea.Model, tea.Cmd) {
	m.wizard = nil
	m.mode = viewList

	id, err := m.manager.Create(msg.profile, msg.name, msg.cwd, max(m.width, 80), max(m.height, 24))
	if err != nil {
		m.toast = err.Error()
		return m, nil
	}

	m.sessionProfile[id] = msg.profile.Name
	m.cfg.UpsertSession(config.SessionDef{Name: msg.name, Profile: msg.profile.Name, Cwd: msg.cwd})
	_ = m.cfg.Save()

	return m.openAttach(id, msg.name)
}

func (m *AppModel) relaunchDef(def config.SessionDef) (tea.Model, tea.Cmd) {
	profile, ok := m.cfg.Profile(def.Profile)
	if !ok {
		m.toast = "profile \"" + def.Profile + "\" no longer exists in config"
		return m, nil
	}

	id, err := m.manager.Create(profile, def.Name, def.Cwd, max(m.width, 80), max(m.height, 24))
	if err != nil {
		m.toast = err.Error()
		return m, nil
	}
	m.sessionProfile[id] = def.Profile
	return m.openAttach(id, def.Name)
}

func (m *AppModel) applyRename(row Row, newName string) {
	oldName := row.Name()
	if row.Live {
		_ = m.manager.Rename(row.Summary.ID, newName)
		profile := m.sessionProfile[row.Summary.ID]
		m.cfg.RemoveSession(oldName)
		m.cfg.UpsertSession(config.SessionDef{Name: newName, Profile: profile, Cwd: row.Summary.Cwd})
	} else {
		m.cfg.RemoveSession(oldName)
		m.cfg.UpsertSession(config.SessionDef{Name: newName, Profile: row.Def.Profile, Cwd: row.Def.Cwd})
	}
	_ = m.cfg.Save()
	m.refreshList()
}

// dispatch routes anything not handled above (key presses, ticks, etc.) to
// whichever sub-model is currently active.
func (m *AppModel) dispatch(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case viewAttached:
		if m.attach != nil {
			var cmd tea.Cmd
			m.attach, cmd = m.attach.Update(msg)
			return m, cmd
		}
	case viewNewSession:
		if m.wizard != nil {
			var cmd tea.Cmd
			m.wizard, cmd = m.wizard.Update(msg)
			return m, cmd
		}
	default:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *AppModel) View() string {
	switch m.mode {
	case viewAttached:
		if m.attach != nil {
			return m.attach.View()
		}
	case viewNewSession:
		if m.wizard != nil {
			return m.wizard.View()
		}
	}
	return m.list.View()
}
