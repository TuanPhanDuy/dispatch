package ptyproc

import (
	"io"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStartCapturesOutput(t *testing.T) {
	p, err := Start("echo", []string{"hello from pty"}, ".", nil, 80, 24)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Close()

	out := make(chan string, 1)
	go func() {
		buf, _ := io.ReadAll(p)
		out <- string(buf)
	}()

	if err := p.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	select {
	case got := <-out:
		if !strings.Contains(got, "hello from pty") {
			t.Fatalf("expected output to contain %q, got %q", "hello from pty", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out reading PTY output")
	}
}

func TestResizeAppliesWindowSize(t *testing.T) {
	p, err := Start("sleep", []string{"0.2"}, ".", nil, 80, 24)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Close()

	if err := p.Resize(120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	_ = p.Wait()
}

func TestWriteForwardsInputToChild(t *testing.T) {
	// cat echoes stdin back to stdout; use it to prove Write() reaches the child.
	p, err := Start("cat", nil, ".", nil, 80, 24)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Close()

	if _, err := p.Write([]byte("ping\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	buf := make([]byte, 64)
	readDone := make(chan struct{})
	var n int
	go func() {
		n, _ = p.Read(buf)
		close(readDone)
	}()

	select {
	case <-readDone:
		if !strings.Contains(string(buf[:n]), "ping") {
			t.Fatalf("expected echoed input to contain %q, got %q", "ping", string(buf[:n]))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for echoed input")
	}

	_ = p.Signal(syscall.SIGTERM)
	_ = p.Wait()
}
