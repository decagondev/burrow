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
