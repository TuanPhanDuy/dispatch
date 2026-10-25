package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// MetaKey is the single reserved escape hatch while attached to a session:
// everything else is forwarded raw to the PTY. ASCII FS (0x1C), sent by the
// terminal for Ctrl-\, was chosen because it's not used by shells or the
// agent CLIs (claude/codex/aider) for anything, unlike Ctrl-a/Ctrl-b which
// collide with readline/tmux-adjacent muscle memory.
const MetaKey = tea.KeyCtrlBackslash

// arrowAndEditingSequences maps non-rune key types to the raw ANSI byte
// sequence a real terminal would have sent, so the child process (which may
// itself be a full-screen TUI like claude/codex) sees identical input to
// running it directly in a terminal.
var arrowAndEditingSequences = map[tea.KeyType]string{
	tea.KeyUp:       "\x1b[A",
	tea.KeyDown:     "\x1b[B",
	tea.KeyRight:    "\x1b[C",
	tea.KeyLeft:     "\x1b[D",
	tea.KeyHome:     "\x1b[H",
	tea.KeyEnd:      "\x1b[F",
	tea.KeyPgUp:     "\x1b[5~",
	tea.KeyPgDown:   "\x1b[6~",
	tea.KeyDelete:   "\x1b[3~",
	tea.KeyInsert:   "\x1b[2~",
	tea.KeyShiftTab: "\x1b[Z",
	tea.KeyF1:       "\x1bOP",
	tea.KeyF2:       "\x1bOQ",
	tea.KeyF3:       "\x1bOR",
	tea.KeyF4:       "\x1bOS",
	tea.KeyF5:       "\x1b[15~",
	tea.KeyF6:       "\x1b[17~",
	tea.KeyF7:       "\x1b[18~",
	tea.KeyF8:       "\x1b[19~",
	tea.KeyF9:       "\x1b[20~",
	tea.KeyF10:      "\x1b[21~",
	tea.KeyF11:      "\x1b[23~",
	tea.KeyF12:      "\x1b[24~",
}

// keyToBytes reconstructs the raw bytes a terminal would have sent for msg,
// so it can be written straight to a child PTY. Control keys (Ctrl-A..Z,
// Esc, Tab, Enter, Backspace, etc.) map 1:1 to their ASCII code since
// Bubble Tea's KeyType already encodes that value; printable runes and
// Alt-modified runes are encoded as UTF-8 (with a leading ESC for Alt).
func keyToBytes(msg tea.KeyMsg) []byte {
	if seq, ok := arrowAndEditingSequences[msg.Type]; ok {
		return []byte(seq)
	}

	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace:
		runes := msg.Runes
		if msg.Type == tea.KeySpace && len(runes) == 0 {
			runes = []rune{' '}
		}
		b := []byte(string(runes))
		if msg.Alt {
			b = append([]byte{0x1b}, b...)
		}
		return b
	}

	// All remaining KeyTypes (control keys 0-31/127, Esc, Tab, Enter,
	// Backspace, Ctrl+letter, etc.) carry their ASCII value directly in
	// the KeyType's int representation.
	if msg.Type >= 0 && msg.Type <= 127 {
		b := []byte{byte(msg.Type)}
		if msg.Alt {
			b = append([]byte{0x1b}, b...)
		}
		return b
	}

	return nil
}
