package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/TuanPhanDuy/dispatch/internal/config"
)

type wizardStep int

const (
	stepProfile wizardStep = iota
	stepName
	stepCwd
)

// NewSessionModel is the 3-step "create a session" wizard: pick a profile,
// name it, pick a working directory.
type NewSessionModel struct {
	profiles []config.AgentProfile
	cursor   int
	step     wizardStep

	name textinput.Model
	cwd  textinput.Model
}

func NewNewSessionModel(profiles []config.AgentProfile, defaultCwd string) *NewSessionModel {
	name := textinput.New()
	cwd := textinput.New()
	cwd.SetValue(defaultCwd)

	return &NewSessionModel{profiles: profiles, name: name, cwd: cwd}
}

func (m *NewSessionModel) Update(msg tea.Msg) (*NewSessionModel, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch m.step {
	case stepProfile:
		return m.updateProfileStep(keyMsg)
	case stepName:
		return m.updateNameStep(keyMsg)
	case stepCwd:
		return m.updateCwdStep(keyMsg)
	}
	return m, nil
}

func (m *NewSessionModel) updateProfileStep(keyMsg tea.KeyMsg) (*NewSessionModel, tea.Cmd) {
	switch keyMsg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.profiles)-1 {
			m.cursor++
		}
	case "enter":
		if len(m.profiles) == 0 {
			return m, nil
		}
		m.name.SetValue(m.profiles[m.cursor].Name)
		m.name.Focus()
		m.step = stepName
	case "esc":
		return m, func() tea.Msg { return cancelWizardMsg{} }
	}
	return m, nil
}

func (m *NewSessionModel) updateNameStep(keyMsg tea.KeyMsg) (*NewSessionModel, tea.Cmd) {
	switch keyMsg.String() {
	case "enter":
		m.name.Blur()
		m.cwd.Focus()
		m.step = stepCwd
		return m, nil
	case "esc":
		return m, func() tea.Msg { return cancelWizardMsg{} }
	}
	var cmd tea.Cmd
	m.name, cmd = m.name.Update(keyMsg)
	return m, cmd
}

func (m *NewSessionModel) updateCwdStep(keyMsg tea.KeyMsg) (*NewSessionModel, tea.Cmd) {
	switch keyMsg.String() {
	case "enter":
		profile := m.profiles[m.cursor]
		name, cwd := m.name.Value(), m.cwd.Value()
		return m, func() tea.Msg {
			return createSessionMsg{profile: profile, name: name, cwd: cwd}
		}
	case "esc":
		return m, func() tea.Msg { return cancelWizardMsg{} }
	}
	var cmd tea.Cmd
	m.cwd, cmd = m.cwd.Update(keyMsg)
	return m, cmd
}

func (m *NewSessionModel) View() string {
	switch m.step {
	case stepProfile:
		var b strings.Builder
		b.WriteString("New session — pick a profile (↑/↓, enter, esc to cancel):\n\n")
		if len(m.profiles) == 0 {
			b.WriteString("  (no profiles configured — edit ~/.config/dispatch/config.yaml)\n")
		}
		for i, p := range m.profiles {
			cursor := "  "
			if i == m.cursor {
				cursor = "> "
			}
			account := ""
			if p.Account != "" {
				account = " @" + p.Account
			}
			fmt.Fprintf(&b, "%s%s (%s)%s\n", cursor, p.Name, strings.TrimSpace(p.Command+" "+strings.Join(p.Args, " ")), account)
		}
		return b.String()
	case stepName:
		return "Session name:\n\n" + m.name.View() + "\n\n(enter to continue, esc to cancel)"
	case stepCwd:
		return "Working directory:\n\n" + m.cwd.View() + "\n\n(enter to create, esc to cancel)"
	}
	return ""
}
