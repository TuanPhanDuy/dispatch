package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKeyToBytesPrintableRune(t *testing.T) {
	got := keyToBytes(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if string(got) != "a" {
		t.Fatalf("got %q, want %q", got, "a")
	}
}

func TestKeyToBytesAltRune(t *testing.T) {
	got := keyToBytes(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}, Alt: true})
	if string(got) != "\x1ba" {
		t.Fatalf("got %q, want %q", got, "\x1ba")
	}
}

func TestKeyToBytesControlKeys(t *testing.T) {
	cases := []struct {
		name string
		typ  tea.KeyType
		want string
	}{
		{"enter", tea.KeyEnter, "\r"},
		{"tab", tea.KeyTab, "\t"},
		{"esc", tea.KeyEsc, "\x1b"},
		{"backspace", tea.KeyBackspace, "\x7f"},
		{"ctrl+c", tea.KeyCtrlC, "\x03"},
		{"ctrl+backslash (meta)", tea.KeyCtrlBackslash, "\x1c"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := keyToBytes(tea.KeyMsg{Type: c.typ})
			if string(got) != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestKeyToBytesArrowsAndEditing(t *testing.T) {
	cases := []struct {
		typ  tea.KeyType
		want string
	}{
		{tea.KeyUp, "\x1b[A"},
		{tea.KeyDown, "\x1b[B"},
		{tea.KeyRight, "\x1b[C"},
		{tea.KeyLeft, "\x1b[D"},
		{tea.KeyHome, "\x1b[H"},
		{tea.KeyEnd, "\x1b[F"},
		{tea.KeyDelete, "\x1b[3~"},
	}
	for _, c := range cases {
		got := keyToBytes(tea.KeyMsg{Type: c.typ})
		if string(got) != c.want {
			t.Fatalf("type %v: got %q, want %q", c.typ, got, c.want)
		}
	}
}

func TestMetaKeyIsCtrlBackslash(t *testing.T) {
	if MetaKey != tea.KeyCtrlBackslash {
		t.Fatalf("MetaKey = %v, want tea.KeyCtrlBackslash", MetaKey)
	}
}
