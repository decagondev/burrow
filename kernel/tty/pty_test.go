package tty

import (
	"errors"
	"syscall"
	"testing"
	"time"
)

func masterRead(t *testing.T, m *Master) string {
	t.Helper()
	p := make([]byte, 256)
	done := make(chan string)
	go func() { k, _ := m.Read(p); done <- string(p[:k]) }()
	select {
	case s := <-done:
		return s
	case <-time.After(time.Second):
		t.Fatal("master read blocked")
		return ""
	}
}

// What the master writes is the slave's typing: echoed back to the master
// with NL as CR NL (OPOST|ONLCR), a whole line for a canonical read.
func TestPairLineAndEcho(t *testing.T) {
	m := NewPair(24, 80)
	s := m.Slave()
	m.OpenSlave()
	m.Write([]byte("ls -l\n"))
	if got := masterRead(t, m); got != "ls -l\r\n" {
		t.Errorf("echo %q, want CR NL", got)
	}
	if got := read(t, s, 64); got != "ls -l\n" {
		t.Errorf("slave read %q", got)
	}
	s.WriteCancel([]byte("a\nb\n"), nil)
	if got := masterRead(t, m); got != "a\r\nb\r\n" {
		t.Errorf("program output %q, want ONLCR", got)
	}
	tm := s.Termios()
	tm.Oflag &^= OPOST
	s.SetTermios(tm, false)
	s.WriteCancel([]byte("raw\n"), nil)
	if got := masterRead(t, m); got != "raw\n" {
		t.Errorf("without OPOST %q", got)
	}
	if rev, _ := m.Poll(); rev != PollOut {
		t.Errorf("master Poll with nothing to read = %#x", rev)
	}
}

// ^C typed into the master signals the slave's foreground group; a new
// size signals SIGWINCH.
func TestPairSignals(t *testing.T) {
	m := NewPair(24, 80)
	s := m.Slave()
	got := make(chan [2]int, 4)
	s.Signal = func(pgrp, sig int) { got <- [2]int{pgrp, sig} }
	s.SetForeground(7)
	m.Write([]byte{0x03})
	if g := <-got; g != [2]int{7, SIGINT} {
		t.Errorf("^C sent %v", g)
	}
	s.SetSize(30, 100)
	if g := <-got; g != [2]int{7, SIGWINCH} {
		t.Errorf("resize sent %v", g)
	}
	s.SetSize(30, 100) // the same size: no signal
	select {
	case g := <-got:
		t.Errorf("unchanged size sent %v", g)
	case <-time.After(50 * time.Millisecond):
	}
}

// When the last slave closes, the master reads what is left, then EIO; when
// the master closes, the slave reads end of file and writes EIO, and its
// foreground group gets SIGHUP.
func TestPairHangups(t *testing.T) {
	m := NewPair(24, 80)
	s := m.Slave()
	m.OpenSlave()
	m.OpenSlave()
	s.WriteCancel([]byte("bye"), nil)
	m.CloseSlave()
	m.CloseSlave()
	if got := masterRead(t, m); got != "bye" {
		t.Errorf("left over %q", got)
	}
	if _, err := m.Read(make([]byte, 8)); !errors.Is(err, syscall.EIO) {
		t.Errorf("master read with no slave = %v, want EIO", err)
	}
	if rev, _ := m.Poll(); rev&PollIn == 0 || rev&PollHup == 0 {
		t.Errorf("master Poll after the slaves closed = %#x", rev)
	}

	m = NewPair(24, 80)
	s = m.Slave()
	sig := make(chan int, 1)
	s.Signal = func(_, n int) { sig <- n }
	s.SetForeground(3)
	m.OpenSlave()
	m.Close()
	if got := read(t, s, 8); got != "" {
		t.Errorf("slave read after hang-up %q", got)
	}
	if _, err := s.WriteCancel([]byte("x"), nil); !errors.Is(err, syscall.EIO) {
		t.Errorf("slave write after hang-up = %v, want EIO", err)
	}
	if n := <-sig; n != SIGHUP {
		t.Errorf("hang-up sent signal %d", n)
	}
}

// A program writing more than the master holds waits for st to read; echo
// never waits (it would deadlock a single-threaded terminal emulator).
func TestPairBackpressure(t *testing.T) {
	m := NewPair(24, 80)
	s := m.Slave()
	m.OpenSlave()
	big := make([]byte, MasterBuffer+10)
	done := make(chan struct{})
	go func() { s.WriteCancel(big, nil); close(done) }()
	select {
	case <-done:
		t.Fatal("a write past the buffer did not wait")
	case <-time.After(50 * time.Millisecond):
	}
	p := make([]byte, MasterBuffer)
	n := 0
	for n < len(big) {
		k, _ := m.Read(p)
		n += k
	}
	<-done
	cancel := make(chan struct{})
	close(cancel)
	s.WriteCancel(make([]byte, MasterBuffer), nil)
	if _, err := s.WriteCancel([]byte("x"), cancel); !errors.Is(err, syscall.EINTR) {
		t.Errorf("a waiting write cancelled = %v, want EINTR", err)
	}
	m.Write([]byte("echo\n")) // returns although the buffer is full
}
