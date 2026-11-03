package proc_test

import (
	"testing"
	"time"

	"burrow/kernel/proc"
	"burrow/kernel/tty"
)

func TestTerminalAsStdio(t *testing.T) {
	r := newRig(t)
	term := tty.New(r.console, 30, 100)
	r.sys.SetTerminal(term)
	p := r.spawn()
	term.SetForeground(p.PGID)

	if r.call(p, 16, 0, 0x5401, scratch) != 0 { // TCGETS: fd 0 is a terminal now
		t.Fatal("TCGETS")
	}
	b := make([]byte, 36)
	p.Mem().CopyIn(scratch, b)
	if le.Uint32(b[12:])&tty.ICANON == 0 || b[17+tty.VINTR] != 3 {
		t.Errorf("termios lflag %#x", le.Uint32(b[12:]))
	}
	r.call(p, 16, 1, 0x5413, scratch)
	p.Mem().CopyIn(scratch, b[:4])
	if le.Uint16(b) != 30 || le.Uint16(b[2:]) != 100 {
		t.Errorf("winsize %dx%d", le.Uint16(b), le.Uint16(b[2:]))
	}
	if r.call(p, 16, 0, 0x540F, scratch) != 0 { // TIOCGPGRP
		t.Fatal("TIOCGPGRP")
	}
	p.Mem().CopyIn(scratch, b[:4])
	if int(le.Uint32(b)) != p.PGID {
		t.Errorf("foreground %d", le.Uint32(b))
	}

	r.nu.Inject(proc.Request{TID: p.TID(), Nr: 0, Args: [6]uint64{0, scratch + 64, 100}})
	time.Sleep(10 * time.Millisecond) // waiting for a line
	for _, c := range []byte("hi\r") {
		term.Input(c)
	}
	if n := r.await(p.TID()); n != 3 {
		t.Fatalf("read = %d", n)
	}
	got := make([]byte, 3)
	p.Mem().CopyIn(scratch+64, got)
	if string(got) != "hi\n" || r.console.String() != "hi\n" {
		t.Errorf("read %q, echo %q", got, r.console.String())
	}

	r.running(p)
	term.Input(0x03) // ^C: SIGINT to the foreground group, default action
	if st, _ := wait(t, p); st != 130 {
		t.Errorf("^C: status %d, want 130", st)
	}
}

func TestProcessGroups(t *testing.T) {
	r := newRig(t)
	p := r.spawn()
	if r.call(p, 111) != int64(p.PID) || r.call(p, 124, 0) != int64(p.PID) {
		t.Error("a spawned program leads its group and session")
	}
	child := r.call(p, 57)
	kid := r.newestThread(p.TID())
	if r.callAs(kid, 111) != int64(p.PID) {
		t.Error("the child starts in the parent's group")
	}
	if r.call(p, 109, uint64(child), uint64(child)) != 0 || r.callAs(kid, 121, 0) != child {
		t.Error("setpgid(child, child)")
	}
	if r.call(p, 112) >= 0 {
		t.Error("setsid by a group leader must fail")
	}
}
