package tui

import "github.com/charmbracelet/lipgloss"

var (
	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("62")).
			Padding(0, 1)

	commandModeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("230")).
				Background(lipgloss.Color("208")).
				Bold(true).
				Padding(0, 1)

	attentionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true)

	helpHintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))
)
