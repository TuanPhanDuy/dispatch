package ptyproc

import "testing"

func TestRingBufferWritesAndSnapshot(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Write([]byte("line1\nline2\npartial"))
	got := rb.Snapshot()
	want := "line1\nline2\npartial"
	if got != want {
		t.Fatalf("Snapshot() = %q, want %q", got, want)
	}
}

func TestRingBufferEvictsOldestLines(t *testing.T) {
	rb := NewRingBuffer(2)
	rb.Write([]byte("a\nb\nc\n"))
	got := rb.Snapshot()
	want := "b\nc\n"
	if got != want {
		t.Fatalf("Snapshot() = %q, want %q", got, want)
	}
}

func TestRingBufferPreservesANSICodes(t *testing.T) {
	rb := NewRingBuffer(10)
	colored := "\x1b[31mred\x1b[0m\n"
	rb.Write([]byte(colored))
	if got := rb.Snapshot(); got != colored {
		t.Fatalf("Snapshot() = %q, want %q", got, colored)
	}
}
