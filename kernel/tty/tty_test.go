package tty

import (
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type buf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (w *buf) Write(p []byte) (int, error) { w.mu.Lock(); defer w.mu.Unlock(); return w.b.Write(p) }
func (w *buf) String() string              { w.mu.Lock(); defer w.mu.Unlock(); return w.b.String() }

func typeIn(t *TTY, s string) {
	for i := 0; i < len(s); i++ {
		t.Input(s[i])
	}
}

func read(t *testing.T, tt *TTY, n int) string {
	t.Helper()
	p := make([]byte, n)
	done := make(chan string)
	go func() { k, _ := tt.Read(p); done <- string(p[:k]) }()
	select {
	case s := <-done:
		return s
	case <-time.After(time.Second):
		t.Fatal("read blocked")
		return ""
	}
}

func TestCanonicalLineEditing(t *testing.T) {
	out := &buf{}
	tt := New(out, 25, 80)
	typeIn(tt, "lsx\x7f -l\r")
	if got := read(t, tt, 64); got != "ls -l\n" {
		t.Errorf("line %q", got)
	}
	if out.String() != "lsx\b \b -l\n" {
		t.Errorf("echo %q", out.String())
	}
	typeIn(tt, "oops\x15ok\n")
	if got := read(t, tt, 64); got != "ok\n" {
		t.Errorf("after ^U %q", got)
	}
	typeIn(tt, "two\nlines\n")
	if a, b := read(t, tt, 64), read(t, tt, 64); a != "two\n" || b != "lines\n" {
		t.Errorf("one line per read: %q %q", a, b)
	}
	typeIn(tt, "abcdef\n")
	if a, b := read(t, tt, 3), read(t, tt, 64); a != "abc" || b != "def\n" {
		t.Errorf("short reads %q %q", a, b)
	}
}

func TestEOFAndBlocking(t *testing.T) {
	tt := New(&buf{}, 25, 80)
	typeIn(tt, "part\x04")
	if got := read(t, tt, 64); got != "part" {
		t.Errorf("^D after text: %q (no newline, no EOF)", got)
	}
	tt.Input(0x04)
	if got := read(t, tt, 64); got != "" {
		t.Errorf("^D on an empty line: %q, want end of file", got)
	}
	cancel := make(chan struct{})
	done := make(chan error)
	go func() { _, err := tt.ReadCancel(make([]byte, 8), cancel); done <- err }()
	time.Sleep(10 * time.Millisecond)
	close(cancel)
	if err := <-done; err != syscall.EINTR {
		t.Errorf("cancelled read = %v", err)
	}
}

func TestSignalsGoToTheForeground(t *testing.T) {
	out := &buf{}
	tt := New(out, 25, 80)
	sent := make(chan [2]int, 4)
	tt.Signal = func(pgrp, sig int) { sent <- [2]int{pgrp, sig} }
	tt.SetForeground(7)
	typeIn(tt, "half a line\x03")
	if got := <-sent; got != [2]int{7, SIGINT} {
		t.Errorf("^C sent %v", got)
	}
	if !strings.HasSuffix(out.String(), "^C\n") || tt.Pending() != 0 {
		t.Errorf("echo %q, %d bytes left", out.String(), tt.Pending())
	}
	tt.Input(0x1C)
	if got := <-sent; got != [2]int{7, SIGQUIT} {
		t.Errorf("^\\ sent %v", got)
	}
}

func TestRawMode(t *testing.T) {
	out := &buf{}
	tt := New(out, 25, 80)
	typeIn(tt, "ab")
	raw := tt.Termios()
	raw.Lflag &^= ICANON | ECHO | ISIG
	tt.SetTermios(raw, false)
	if got := read(t, tt, 64); got != "ab" {
		t.Errorf("the half line becomes input: %q", got)
	}
	tt.Input(0x03) // not a signal in raw mode
	tt.Input('x')
	if got := read(t, tt, 64); got != "\x03x" || out.String() != "ab" {
		t.Errorf("raw read %q, echo %q", got, out.String())
	}
}

// Non-canonical reads follow VMIN and VTIME (termios(3)): kilo sets
// VMIN=0 VTIME=1 to tell a lone Esc from an arrow key's sequence.
func TestVminVtime(t *testing.T) {
	tt := New(&buf{}, 25, 80)
	raw := tt.Termios()
	raw.Lflag &^= ICANON | ECHO | ISIG
	raw.CC[VMIN], raw.CC[VTIME] = 0, 0
	tt.SetTermios(raw, false)
	p := make([]byte, 8)
	if n, err := tt.Read(p); n != 0 || err != nil { // polling: nothing there, return at once
		t.Errorf("VMIN=0 VTIME=0 with no input = %d, %v", n, err)
	}
	raw.CC[VTIME] = 1 // 100 ms
	tt.SetTermios(raw, false)
	start := time.Now()
	if n, _ := tt.Read(p); n != 0 || time.Since(start) < 90*time.Millisecond || time.Since(start) > time.Second {
		t.Errorf("VTIME=1 with no input = %d after %v, want 0 after 100 ms", n, time.Since(start))
	}
	done := make(chan int)
	go func() { n, _ := tt.Read(p); done <- n }()
	for !tt.HasReader() { // a timed read still counts as a reader: the console forwards keys to it
		time.Sleep(time.Millisecond)
	}
	tt.Input(0x1B)
	if n := <-done; n != 1 || p[0] != 0x1B {
		t.Errorf("VTIME=1 with a key = %d %q", n, p[:n])
	}
	raw.CC[VMIN], raw.CC[VTIME] = 1, 0 // blocking again
	tt.SetTermios(raw, false)
	go func() { n, _ := tt.Read(p); done <- n }()
	select {
	case n := <-done:
		t.Fatalf("VMIN=1 returned %d with no input", n)
	case <-time.After(150 * time.Millisecond):
	}
	tt.Input('k')
	if n := <-done; n != 1 {
		t.Errorf("VMIN=1 read %d", n)
	}
}
